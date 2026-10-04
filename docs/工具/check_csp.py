"""内容安全策略一致性检查：页面 <meta> 的策略不得比 部署/headers、nginx 示例更宽松，且两处响应头必须一致。
用法：python 工具/check_csp.py（构建脚本会自动调用）。"""
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
# 页面策略比响应头更严格时允许缺少的指令（connect-src 与 default-src 等价；不含 style-src-attr 时行内样式属性被禁止）。
STRICTER_OK = {"connect-src 'self'", "style-src-attr 'unsafe-inline'"}


def directives(policy):
    return {part.strip() for part in policy.split(';') if part.strip()}


def main():
    header = re.search(r'Content-Security-Policy:\s*(.+)', (ROOT / '部署/headers').read_text(encoding='utf-8')).group(1).strip()
    nginx = re.search(r'add_header Content-Security-Policy "([^"]+)"', (ROOT / '部署/nginx.conf.example').read_text(encoding='utf-8')).group(1)
    problems = []
    if directives(header) != directives(nginx):
        problems.append('部署/headers 与 部署/nginx.conf.example 的 CSP 不一致')
    allowed = {item for item in directives(header) if not item.startswith('frame-ancestors')}
    for page in sorted(ROOT.glob('*.html')):
        found = re.search(r'http-equiv="Content-Security-Policy" content="([^"]+)"', page.read_text(encoding='utf-8'))
        if not found:
            problems.append(f'{page.name}: 缺少 CSP meta')
            continue
        meta = directives(found.group(1))
        looser = meta - allowed
        missing = allowed - meta - STRICTER_OK
        if looser: problems.append(f'{page.name}: meta 比响应头更宽松：{sorted(looser)}')
        if missing: problems.append(f'{page.name}: meta 缺少响应头中的指令：{sorted(missing)}')
    if problems:
        print('FAIL: CSP 漂移\n  ' + '\n  '.join(problems))
        sys.exit(1)
    print('PASS: CSP meta、响应头与 nginx 示例一致')


if __name__ == '__main__':
    main()
