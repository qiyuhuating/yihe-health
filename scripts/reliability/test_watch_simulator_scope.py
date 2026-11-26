import unittest

from watch_simulator_scope import documentation_only


class WatchSimulatorScopeTest(unittest.TestCase):
    def test_only_added_or_modified_regular_documentation_skips_the_matrix(self):
        modified = b":100644 100644 1111111 2222222 M\0"
        added = b":000000 100644 0000000 2222222 A\0"
        docs = modified + b"README.md\0" + added + b"docs/install.md\0"
        self.assertTrue(documentation_only(docs))

        cases = {
            "empty": b"",
            "mixed source and docs": docs + modified + b"scripts/tool.py\0",
            "deleted docs": b":100644 000000 1111111 0000000 D\0docs/install.md\0",
            "new symlink": b":000000 120000 0000000 2222222 A\0docs/install.md\0",
        }
        for name, diff in cases.items():
            with self.subTest(name=name):
                self.assertFalse(documentation_only(diff))
