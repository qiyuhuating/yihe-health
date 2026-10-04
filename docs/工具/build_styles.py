"""Build page CSS bundles from deduplicated source fragments.

`docs/样式源` is the editable source of truth. `docs/样式/{page}.css` are generated
single-request bundles kept for local static preview and copied verbatim to releases.
"""
from __future__ import annotations

import argparse
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SRC = ROOT / '样式源'
OUT = ROOT / '样式'

HEADERS = {
    'site.css': '/* 官网样式（home / about / services / contact / guide / privacy）。\n   由去重样式源构建；请勿直接编辑此生成文件。 */',
    'login.css': '/* 居民登录页样式。由去重样式源构建；请勿直接编辑此生成文件。 */',
    'resident.css': '/* 居民端样式。由去重样式源构建；请勿直接编辑此生成文件。 */',
    'staff.css': '/* 工作人员端样式。由去重样式源构建；请勿直接编辑此生成文件。 */',
}
BUNDLES = {
    'site.css': ['reconstruction-site.css', 'ui-polish.css', 'xz-system.css'],
    'login.css': ['reconstruction-site.css', 'ui-polish.css', 'xinzhongshi.css', 'xz-system.css'],
    'resident.css': ['reconstruction-care.css', 'community.css', 'ui-polish.css', 'xinzhongshi.css', 'xz-system.css'],
    'staff.css': ['reconstruction-care.css', 'community.css', 'ui-polish.css', 'xinzhongshi.css', 'xinzhongshi-staff.css', 'xz-system.css'],
}


def render(name: str) -> str:
    chunks = [HEADERS[name], '']
    for fragment in BUNDLES[name]:
        body = (SRC / fragment).read_text(encoding='utf-8').strip()
        label = fragment.replace('reconstruction-site.css', 'reconstruction.css').replace('reconstruction-care.css', 'reconstruction.css')
        chunks.extend([f'/* ── {label} ── */', body, ''])
    return '\n'.join(chunks).rstrip() + '\n'


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args(argv)
    OUT.mkdir(exist_ok=True)
    stale = []
    for name in BUNDLES:
        expected = render(name)
        path = OUT / name
        if args.check:
            if not path.exists() or path.read_text(encoding='utf-8') != expected:
                stale.append(name)
        else:
            path.write_text(expected, encoding='utf-8')
    if stale:
        raise SystemExit('Stale generated CSS bundles: ' + ', '.join(stale))
    print('PASS: deduplicated CSS source -> single-bundle page CSS' if not args.check else 'PASS: generated CSS bundles match deduplicated sources')


if __name__ == '__main__':
    main()
