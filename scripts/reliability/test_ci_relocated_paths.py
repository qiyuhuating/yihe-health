"""Guard integration paths after moving backend and frontend to root."""
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[2]


class RelocatedCIPaths(unittest.TestCase):
    def test_real_watch_integration_builds_from_backend(self):
        source = (ROOT / '.github/workflows/reusable-watch.yml').read_text(encoding='utf-8')
        self.assertIn('(cd backend && go build -o', source)
        self.assertTrue((ROOT / 'backend/cmd/server/main.go').is_file())
        self.assertNotIn('(# Work from the repository root', source)

    def test_ui_baseline_resolves_from_frontend(self):
        workflow = (ROOT / '.github/workflows/ui-quality.yml').read_text(encoding='utf-8')
        path = 'node scripts/visual-rmse.mjs ../docs/screenshots/login-desktop.png ../tests/ui/artifacts/login-desktop.png'
        self.assertIn(path, workflow)
        self.assertTrue((ROOT / 'frontend/scripts/visual-rmse.mjs').is_file())
        self.assertTrue((ROOT / 'docs/screenshots/login-desktop.png').is_file())
        self.assertTrue((ROOT / 'tests/ui/ui-smoke.mjs').is_file())

    def test_real_backend_and_frontend_paths(self):
        self.assertTrue((ROOT / 'frontend/package.json').is_file())
        self.assertTrue((ROOT / 'backend/go.mod').is_file())
        for name in ['ci.yml', 'reusable-backend.yml', 'reusable-frontend.yml']:
            workflow = (ROOT / '.github/workflows' / name).read_text(encoding='utf-8')
            self.assertNotIn('../../evidence/', workflow)
            self.assertNotIn('../../scripts/', workflow)


if __name__ == '__main__':
    unittest.main()