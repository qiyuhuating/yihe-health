"""Source and release SHA-256 manifests, with explicit maintainer refresh."""
import argparse
import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
IGNORED_PARTS = {'.git', '__pycache__', '.venv', 'node_modules', '.test-results', 'htmlcov'}
GENERATED = {'SOURCE-MANIFEST.json', 'coverage.json', 'coverage.xml'}


def version(root=ROOT):
    return (root / 'VERSION').read_text(encoding='utf-8').strip()


def build_revision(root=ROOT):
    return (root / 'BUILD-REVISION').read_text(encoding='utf-8').strip()


def source_files(root=ROOT):
    result = {}
    for path in sorted(root.rglob('*')):
        relative = path.relative_to(root)
        if set(relative.parts) & IGNORED_PARTS or relative.parts[:2] == ('docs', 'dist'):
            continue
        if relative.as_posix() in GENERATED or path.name.startswith('.coverage'):
            continue
        if path.is_symlink(): raise ValueError('Source symlink: ' + relative.as_posix())
        if path.is_file():
            if '.sqlite3' in path.name or '.db' in path.name:
                raise ValueError('Database must be outside the source package: ' + relative.as_posix())
            result[relative.as_posix()] = hashlib.sha256(path.read_bytes()).hexdigest()
    return result


def verify_source(root=ROOT):
    manifest = json.loads((root / 'SOURCE-MANIFEST.json').read_text(encoding='utf-8'))
    current = version(root)
    if (manifest['version'] != current or manifest.get('buildRevision') != build_revision(root)
            or manifest['files'] != source_files(root)
            or not (root / 'README.md').read_text(encoding='utf-8').startswith('# 颐和健康 v' + current + '\n')
            or not (root / 'CHANGELOG.md').read_text(encoding='utf-8').startswith('# 更新记录\n\n## ' + current + '（')):
        raise ValueError('Source manifest mismatch; review source changes before an explicit --write-source')
    print('PASS: source manifest,', len(manifest['files']), 'files, v' + version(root))


def write_source(root=ROOT):
    path = root / 'SOURCE-MANIFEST.json'
    manifest = json.loads(path.read_text(encoding='utf-8')) if path.exists() else {}
    manifest.update(version=version(root), buildRevision=build_revision(root), files=source_files(root))
    path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')


def verify_frontend(root=ROOT):
    output = root / 'docs/dist'
    manifest = json.loads((output / 'release-manifest.json').read_text(encoding='utf-8'))
    files = {path.relative_to(output).as_posix(): hashlib.sha256(path.read_bytes()).hexdigest()
             for path in sorted(output.rglob('*')) if path.is_file() and path.name != 'release-manifest.json'}
    if (manifest.get('version') != version(root) or manifest.get('tag') != 'v' + version(root)
            or manifest.get('buildRevision') != build_revision(root) or manifest.get('mode') != 'http' or manifest['files'] != files):
        raise ValueError('Frontend release manifest mismatch')
    print('PASS: release manifest,', len(files), 'files, v' + version(root))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--write-source', action='store_true')
    parser.add_argument('--frontend', action='store_true')
    args = parser.parse_args()
    if args.write_source: write_source()
    verify_source()
    if args.frontend: verify_frontend()


if __name__ == '__main__': main()
