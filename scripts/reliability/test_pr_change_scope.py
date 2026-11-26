"""Keep the selective gate conservative without running unrelated suites."""

import unittest

from pr_change_scope import classify, docs_only, is_documentation


class ChangeScopeTests(unittest.TestCase):
    def test_docs_only(self):
        paths = ["docs/ARCHITECTURE.md", "README.md", ".github/ISSUE_TEMPLATE/config.yml"]
        self.assertTrue(docs_only(paths))
        self.assertFalse(any(classify(paths)[key] for key in ("backend", "frontend", "watch")))
        self.assertFalse(docs_only([]))
        self.assertFalse(is_documentation(".github/ISSUE_TEMPLATE/script.sh"))

    def test_independent_subsystems(self):
        cases = {
            "frontend/src/views/Login.vue": ("frontend", "ui", "codeql_js"),
            "backend/internal/care/workflow.go": ("backend", "windows", "codeql_go", "security_scan"),
            "YiheWatch/entry/src/main/ets/pages/Index.ets": ("watch", "simulator"),
            "desktop/src/lib.rs": ("windows", "desktop"),
            "deploy/platform.compose.yml": ("deployment",),
            "tests/ui/ui-smoke.mjs": ("frontend", "ui"),
        }
        for path, expected in cases.items():
            with self.subTest(path=path):
                got = classify([path])
                self.assertFalse(got["docs_only"])
                self.assertEqual({key for key in got if got[key]}, set(expected) | ({"fast_gate"} if path.startswith(("backend/", "frontend/")) else set()))

    def test_protocol_and_dependency_changes(self):
        got = classify(["backend/internal/handlers/handler_device_report.go"])
        self.assertTrue(got["watch"])
        self.assertTrue(got["simulator"])
        self.assertTrue(classify(["frontend/package-lock.json"])["dependency"])
        self.assertTrue(classify(["backend/go.mod"])["dependency"])
        self.assertTrue(classify(["backend/internal/apidoc/contracts.json"])["frontend"])
        self.assertFalse(classify(["backend/internal/care/workflow.go"])["watch"])

    def test_workflow_unknown_and_dispatch_fail_closed(self):
        for paths in ([], [".github/workflows/ci.yml"], ["scripts/reliability/pr_change_scope.py"], ["unknown-new-config.yml"]):
            with self.subTest(paths=paths):
                flags = classify(paths)
                self.assertFalse(flags["docs_only"])
                self.assertTrue(all(flags[key] for key in ("backend", "frontend", "watch", "windows", "deployment", "security_scan")))

    def test_mixed_docs_do_not_expand_scope(self):
        self.assertEqual(classify(["README.md", "frontend/src/App.vue"]),
                         classify(["frontend/src/App.vue"]))


if __name__ == "__main__":
    unittest.main()
