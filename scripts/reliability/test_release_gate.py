import copy
import importlib.util
import re
from pathlib import Path
import unittest
import urllib.parse

spec = importlib.util.spec_from_file_location("release_gate", Path(__file__).with_name("release-gate.py"))
gate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gate)
SHA = "a"*40
CI_JOBS = (
    "Core CI / required", "Frontend / Build and dependency audit", "UI Quality / verify",
    f"Backend / Test, build and vet (Go {gate.BACKEND_GO_VERSION})",
    "Watch protocol / Protocol and dependency audit", "Watch protocol / Real backend and lost-ack recovery",
    "Watch protocol / HarmonyOS SDK compile (no hardware)", "Watch simulator",
    "Windows sidecar contract / verify", "Windows sidecar contract / Tauri build and installation",
    "Deployment and alerts / compose-and-alert-rules", "Current-source vulnerability scan",
)
ARTIFACT_PREFIXES = (f"backend-{gate.BACKEND_GO_VERSION}-", "frontend-", "ui-quality-",
                     "watch-", "watch-integration-", "watch-native-", "windows-sidecar-",
                     "windows-desktop-", "security-")


class ReleaseGateTests(unittest.TestCase):
    def fixture(self):
        return {
            "branch": SHA,
            "runs": [{"id": i, "head_sha": SHA, "path": path, "status": "completed", "conclusion": "success", "html_url": "https://github.com/example/run",
                      "event": "push", "head_branch": "preview", "run_attempt": 1}
                     for i,path in enumerate((".github/workflows/ci.yml", ".github/workflows/security.yml"), 1)],
            "jobs": [{"name": name, "status": "completed", "conclusion": "success"} for name in CI_JOBS],
            "security_jobs": [{"name": name, "status": "completed", "conclusion": "success"}
                              for name in ("CodeQL / go", "CodeQL / javascript-typescript")],
            "artifacts": [{"id": i, "name": prefix+SHA, "expired": False, "digest": "sha256:"+"b"*64, "size_in_bytes": 42,
                           "workflow_run": {"id": 1, "head_sha": SHA, "head_branch": "preview"}}
                          for i,prefix in enumerate(ARTIFACT_PREFIXES,1)],
        }

    def api(self, data):
        def get(path):
            if "/branches/" in path:
                return {"commit": {"sha": data["branch"]}}
            query = urllib.parse.parse_qs(urllib.parse.urlsplit(path).query)
            page = int(query["page"][0])
            if "/jobs?" in path:
                key = "security_jobs" if "/runs/2/" in path else "jobs"
                items, output = data[key], "jobs"
            elif "/artifacts?" in path:
                items, output = data["artifacts"], "artifacts"
            else:
                items, output = data["runs"], "workflow_runs"
            return {output: items[(page-1)*100:page*100]}
        return get

    def test_accepts_exact_source_evidence(self):
        result = gate.verify("owner/repo", "preview", SHA, self.api(self.fixture()))
        self.assertEqual(result["source_sha"], SHA)
        self.assertEqual(len(result["artifacts"]), len(ARTIFACT_PREFIXES))
        self.assertEqual(len(result["jobs"]), len(CI_JOBS))

    def test_every_required_job_must_pass_even_when_workflow_and_core_gate_are_green(self):
        for name in CI_JOBS:
            for status, conclusion in (("completed", "skipped"), ("completed", "cancelled"),
                                       ("completed", "failure"), ("in_progress", None)):
                with self.subTest(job=name, status=status, conclusion=conclusion):
                    data = self.fixture()
                    next(job for job in data["jobs"] if job["name"] == name).update(status=status, conclusion=conclusion)
                    with self.assertRaises(ValueError):
                        gate.verify("owner/repo", "preview", SHA, self.api(data))

    def test_missing_or_duplicate_required_jobs_fail(self):
        for name in CI_JOBS:
            for duplicate in (False, True):
                with self.subTest(job=name, duplicate=duplicate):
                    data = self.fixture()
                    if duplicate:
                        data["jobs"].append(copy.deepcopy(next(job for job in data["jobs"] if job["name"] == name)))
                    else:
                        data["jobs"] = [job for job in data["jobs"] if job["name"] != name]
                    with self.assertRaises(ValueError):
                        gate.verify("owner/repo", "preview", SHA, self.api(data))

    def test_all_native_ui_and_desktop_evidence_is_required(self):
        for prefix in ARTIFACT_PREFIXES:
            with self.subTest(artifact=prefix):
                data = self.fixture()
                data["artifacts"] = [a for a in data["artifacts"] if a["name"] != prefix + SHA]
                with self.assertRaises(ValueError):
                    gate.verify("owner/repo", "preview", SHA, self.api(data))

    def test_backend_go_version_matches_ci_matrix(self):
        workflow = (Path(__file__).resolve().parents[2] / ".github/workflows/reusable-backend.yml").read_text(encoding="utf-8")
        matrix = re.search(r"^\s*go-version:\s*\[([^]]+)\]", workflow, re.MULTILINE)
        self.assertIsNotNone(matrix, "backend workflow must declare an explicit go-version matrix")
        versions = re.findall(r"\d+\.\d+\.\d+", matrix.group(1))
        self.assertEqual(versions, [gate.BACKEND_GO_VERSION])
        self.assertEqual(len(versions), 1, "backend verification must run exactly one Go version")
        self.assertIn(f"Backend / Test, build and vet (Go {versions[0]})", CI_JOBS)
        self.assertIn(f"backend-{versions[0]}-", ARTIFACT_PREFIXES)

    def test_security_workflow_green_does_not_mask_skipped_codeql(self):
        for job in ("CodeQL / go", "CodeQL / javascript-typescript"):
            data = self.fixture()
            next(item for item in data["security_jobs"] if item["name"] == job)["conclusion"] = "skipped"
            with self.assertRaises(ValueError):
                gate.verify("owner/repo", "preview", SHA, self.api(data))

    def test_only_trusted_preview_push_or_dispatch_runs_qualify(self):
        for update in ({"event": "pull_request"}, {"head_branch": "unreviewed-branch"}, {"head_sha": "b"*40}):
            data = self.fixture()
            data["runs"][0].update(update)
            with self.assertRaises(ValueError):
                gate.verify("owner/repo", "preview", SHA, self.api(data))

    def test_reads_required_jobs_and_artifacts_after_page_one(self):
        data = self.fixture()
        data["jobs"] = [{"name": f"extra {i}", "status": "completed", "conclusion": "success"} for i in range(105)] + data["jobs"]
        data["artifacts"] = [{"name": f"extra {i}", "expired": False} for i in range(105)] + data["artifacts"]
        data["runs"] = [{"id": i+100, "head_sha": SHA, "path": "other.yml", "status": "completed", "conclusion": "success"} for i in range(105)] + data["runs"]
        gate.verify("owner/repo", "preview", SHA, self.api(data))
        next(job for job in data["jobs"] if job["name"] == "UI Quality / verify")["conclusion"] = "failure"
        with self.assertRaises(ValueError):
            gate.verify("owner/repo", "preview", SHA, self.api(data))

    def test_latest_run_attempt_is_requested(self):
        data = self.fixture()
        data["runs"][0]["run_attempt"] = 2
        requests = []
        original = self.api(data)
        def api(path):
            requests.append(path)
            return original(path)
        gate.verify("owner/repo", "preview", SHA, api)
        self.assertTrue(any("/runs/1/attempts/2/jobs?" in path for path in requests))

    def test_rejects_wrong_source_empty_and_duplicate_artifacts(self):
        for update in ({"workflow_run": {}},
                       {"workflow_run": {"id": 1, "head_sha": "b"*40, "head_branch": "preview"}},
                       {"workflow_run": {"id": 99, "head_sha": SHA, "head_branch": "preview"}},
                       {"workflow_run": {"id": 1, "head_sha": SHA, "head_branch": "other"}},
                       {"size_in_bytes": 0}):
            data = self.fixture()
            data["artifacts"][0].update(update)
            with self.assertRaises(ValueError):
                gate.verify("owner/repo", "preview", SHA, self.api(data))
        data = self.fixture()
        data["artifacts"].append(copy.deepcopy(data["artifacts"][0]))
        with self.assertRaises(ValueError):
            gate.verify("owner/repo", "preview", SHA, self.api(data))

    def test_watch_protocol_evidence_is_required(self):
        data=self.fixture()
        data["artifacts"]=[item for item in data["artifacts"] if not item["name"].startswith("watch-")]
        with self.assertRaises(ValueError):
            gate.verify("owner/repo", "preview", SHA, self.api(data))

    def test_rejects_stale_source_missing_runs_and_partial_gate(self):
        for field,value in (("branch", "c"*40), ("runs", []), ("jobs", []), ("artifacts", [])):
            with self.subTest(field=field):
                data=self.fixture(); data[field]=value
                with self.assertRaises(ValueError):
                    gate.verify("owner/repo", "preview", SHA, self.api(data))

    def test_latest_attempt_cannot_reuse_previous_green(self):
        for status,conclusion in (("in_progress",None),("completed","failure"),("completed","cancelled")):
            data=self.fixture()
            newer=copy.deepcopy(data["runs"][0]); newer.update(id=99,status=status,conclusion=conclusion)
            data["runs"].append(newer)
            with self.assertRaises(ValueError):
                gate.verify("owner/repo", "preview", SHA, self.api(data))

    def test_rejects_expired_or_unhashed_artifact(self):
        for update in ({"expired":True},{"digest":None},{"name":"wrong-source"}):
            data=self.fixture(); data["artifacts"][0].update(update)
            with self.assertRaises(ValueError):
                gate.verify("owner/repo", "preview", SHA, self.api(data))

    def test_branch_move_during_verification_fails(self):
        data=self.fixture(); original=self.api(data); count=0
        def api(path):
            nonlocal count
            if "/branches/" in path:
                count+=1
                if count>1: data["branch"]="c"*40
            return original(path)
        with self.assertRaises(ValueError):
            gate.verify("owner/repo", "preview", SHA, api)


    def test_accepts_main_exact_source_evidence(self):
        data = self.fixture()
        for run in data["runs"]:
            run["head_branch"] = "main"
        for artifact in data["artifacts"]:
            artifact["workflow_run"]["head_branch"] = "main"
        result = gate.verify("owner/repo", "main", SHA, self.api(data))
        self.assertEqual(result["branch"], "main")


if __name__ == "__main__":
    unittest.main()
