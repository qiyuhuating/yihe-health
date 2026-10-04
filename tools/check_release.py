"""Local release gate; the same checks are executed by GitHub Actions."""
import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

from manifests import ROOT, verify_frontend, verify_source, version, build_revision


def run(*command):
    subprocess.run(command, cwd=ROOT, check=True)


def check_frontend():
    for directory in (ROOT / 'docs/脚本', ROOT / 'docs/数据', ROOT / 'docs/dist'):
        for path in sorted(directory.rglob('*.js')): run('node', '--check', str(path))
    output = ROOT / 'docs/dist'
    forbidden = {'config.js', 'demo-credentials.js', 'admin-credentials.js', 'residents.js',
                 'seed-data.js', 'metrics.js'}
    for path in output.rglob('*'):
        if not path.is_file(): continue
        if path.name in forbidden or path.suffix in {'.py', '.sqlite3', '.db', '.env'}:
            raise ValueError('Sensitive production file: ' + str(path.relative_to(output)))
        if path.suffix in {'.js', '.html'}:
            text = path.read_text(encoding='utf-8')
            if re.search(r'(?:window\.)?RESIDENT_DATA\s*=|YIHE_DEVICE_KEY|correct-horse-|demo-credentials|admin-credentials', text):
                raise ValueError('Sensitive production content: ' + str(path.relative_to(output)))
    config = (output / 'js/care-runtime-config.js').read_text(encoding='utf-8')
    if "mode:'http'" not in config or "['http']" not in config or "mode:'demo'" in config:
        raise ValueError('Production frontend must be HTTP-only')
    print('PASS: JavaScript syntax and production sensitive-file scan')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--e2e', action='store_true', help='Also run real-backend Chromium regressions and privacy test')
    args = parser.parse_args()
    manifest_path = ROOT / 'docs/dist/release-manifest.json'
    try:
        verify_source()
        run(sys.executable, 'tools/run_unit_tests.py')
        run(sys.executable, 'docs/工具/build_frontend.py')
        check_frontend()
        run('node', 'tests-added/frontend_contract.cjs')
        if args.e2e:
            run(sys.executable, 'tests-added/e2e_render_contract.py')
            run(sys.executable, 'tests-added/e2e_privacy.py')
            run(sys.executable, 'tests-added/e2e_real_backend.py')
            run(sys.executable, 'tests-added/e2e_interaction.py')
            run(sys.executable, 'tests-added/e2e_partial_render.py')
        manifest = json.loads(manifest_path.read_text(encoding='utf-8'))
        manifest['version'] = version()
        manifest['buildRevision'] = build_revision()
        manifest['tag'] = 'v' + version()
        manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
        verify_frontend()
    except BaseException:
        # A failed validation may leave build files, but never a release manifest.
        manifest_path.unlink(missing_ok=True)
        raise


if __name__ == '__main__': main()
