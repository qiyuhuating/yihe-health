#!/usr/bin/env python3
"""Verify current branch SHA, latest CI runs and immutable artifact digests."""
import argparse
import datetime
import json
import os
import re
import urllib.parse
import urllib.request
from pathlib import Path

# The backend CI matrix is the single source of truth; the guard in
# test_release_gate.py fails if this constant drifts from the workflow.
BACKEND_GO_VERSION = "1.27.4"

REQUIRED_CI_JOBS = (
    "Core CI / required",
    "Frontend / Build and dependency audit",
    "UI Quality / verify",
    f"Backend / Test, build and vet (Go {BACKEND_GO_VERSION})",
    "Watch protocol / Protocol and dependency audit",
    "Watch protocol / Real backend and lost-ack recovery",
    "Watch protocol / HarmonyOS SDK compile (no hardware)",
    "Watch simulator",
    "Windows sidecar contract / verify",
    "Windows sidecar contract / Tauri build and installation",
    "Deployment and alerts / compose-and-alert-rules",
    "Current-source vulnerability scan",
)
REQUIRED_SECURITY_JOBS = ("CodeQL / go", "CodeQL / javascript-typescript")
REQUIRED_ARTIFACTS = (
    f"backend-{BACKEND_GO_VERSION}-", "frontend-", "ui-quality-",
    "watch-", "watch-integration-", "watch-native-", "windows-sidecar-",
    "windows-desktop-", "security-",
)


def collection(api, path, key):
    """Read every page; a large run must not hide a failed check after page one."""
    result = []
    separator = "&" if "?" in path else "?"
    for page in range(1, 101):
        items = api(f"{path}{separator}per_page=100&page={page}")[key]
        if not isinstance(items, list):
            raise ValueError("malformed GitHub collection: " + key)
        result.extend(items)
        if len(items) < 100:
            return result
    raise ValueError("GitHub pagination did not terminate: " + key)


def require_jobs(api, base, run, required):
    attempt = run.get("run_attempt", 1)
    if not isinstance(attempt, int) or attempt < 1:
        raise ValueError("invalid workflow attempt")
    path = f"{base}/actions/runs/{run['id']}/attempts/{attempt}/jobs"
    jobs = collection(api, path, "jobs")
    for name in required:
        matches = [job for job in jobs if job["name"] == name]
        if len(matches) != 1:
            raise ValueError("missing or ambiguous required job: " + name)
        job = matches[0]
        if job["status"] != "completed" or job["conclusion"] != "success":
            raise ValueError("required job did not pass: " + name)
    return [{"name": name, "conclusion": "success"} for name in required]


def get_api(path):
    request = urllib.request.Request("https://api.github.com/"+path, headers={
        "Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28",
        "User-Agent": "yihe-exact-source-release-gate",
    })
    token = os.getenv("GITHUB_TOKEN")
    if token:
        request.add_header("Authorization", "Bearer "+token)
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.load(response)


def verify(repo, branch, sha, api=get_api):
    if not re.fullmatch(r"[0-9a-f]{40}", sha):
        raise ValueError("an explicit full lowercase commit SHA is required")
    base = "repos/"+repo
    ref = base+"/branches/"+urllib.parse.quote(branch, safe="")
    if api(ref)["commit"]["sha"] != sha:
        raise ValueError("branch head differs from requested release source")
    # Query all event types. The PR-only connector helper cannot prove that a
    # push-triggered run is absent or successful.
    runs = collection(api, base+"/actions/runs?head_sha="+sha, "workflow_runs")
    selected = []
    for workflow in (".github/workflows/ci.yml", ".github/workflows/security.yml"):
        matching = [run for run in runs if run["head_sha"] == sha and run["path"] == workflow
                    and run.get("event") in ("push", "workflow_dispatch")
                    and run.get("head_branch") == branch]
        if not matching:
            raise ValueError("no exact-source run: "+workflow)
        run = max(matching, key=lambda item: (item["id"], item.get("run_attempt", 1)))
        if run["status"] != "completed" or run["conclusion"] != "success":
            raise ValueError("latest exact-source workflow is not successful: "+workflow)
        selected.append(run)
    ci = selected[0]
    jobs = require_jobs(api, base, ci, REQUIRED_CI_JOBS)
    security_jobs = require_jobs(api, base, selected[1], REQUIRED_SECURITY_JOBS)
    artifacts = collection(api, base+"/actions/runs/"+str(ci["id"])+"/artifacts", "artifacts")
    evidence = []
    for prefix in REQUIRED_ARTIFACTS:
        matches = [a for a in artifacts if a["name"] == prefix+sha and not a["expired"]]
        digest = matches[0].get("digest") if len(matches) == 1 else None
        if not isinstance(digest, str) or not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
            raise ValueError("missing, ambiguous, expired or unhashed evidence artifact: "+prefix+sha)
        if matches[0].get("size_in_bytes", 0) <= 0:
            raise ValueError("empty required evidence artifact: " + prefix + sha)
        artifact_run = matches[0].get("workflow_run", {})
        if (artifact_run.get("head_sha") != sha or artifact_run.get("id") != ci["id"]
                or artifact_run.get("head_branch") != branch):
            raise ValueError("artifact source differs from requested release")
        evidence.append({key: matches[0][key] for key in ("id", "name", "digest", "size_in_bytes")})
    if api(ref)["commit"]["sha"] != sha:
        raise ValueError("branch moved during verification; rerun on the new head")
    return {"source_sha": sha, "branch": branch, "repository": repo,
            "verified_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
            "workflows": [{key: r[key] for key in ("id", "head_sha", "html_url", "conclusion")} for r in selected],
            "artifacts": evidence,
            "jobs": jobs, "security_jobs": security_jobs,
            "scope": "Software build evidence includes UI, native HAP and actual Windows distributions. Simulator flows, data migration, sustained capacity, repository protection and final independent review retain their own acceptance records."}


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", required=True)
    parser.add_argument("--branch", default="preview")
    parser.add_argument("--sha", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    result = verify(args.repo, args.branch, args.sha)
    Path(args.output).write_text(json.dumps(result, ensure_ascii=False, indent=2)+"\n", encoding="utf-8")
    print("Verified exact source and evidence: "+args.sha)
