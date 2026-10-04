"""样式审计：每个页面只加载自己所属的一个样式包，且样式包内没有"被同选择器后续规则覆盖"的死声明、重复规则和空规则。

  python 工具/css_audit.py          检查（构建会自动调用，失败时退出码为 1）
  python 工具/css_audit.py --fix    就地删除死声明与重复规则（会规范化格式，丢弃行内注释）

样式包与页面的对应关系（改页面所属的包，只会影响该包内的页面）：
  site.css      官网 home / about / services / contact / guide / privacy
  login.css     居民登录页
  resident.css  居民端首页
  staff.css     工作人员端
"""
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from css_model import parse, serialize  # noqa: E402

ROOT = Path(__file__).resolve().parents[1]
STYLE = ROOT / '样式'
BUNDLES = {
    'site': ['about', 'contact', 'guide', 'home', 'privacy', 'services'],
    'login': ['login'],
    'resident': ['index'],
    'staff': ['管理端'],
}


def analyse(items):
    """返回 (保留项, 死声明数, 重复规则数, 空规则数, 重复 keyframes 数)。"""
    later, seen_rules, seen_kf = {}, set(), set()
    keep, dead, dup, empty, dup_kf = [None] * len(items), 0, 0, 0, 0
    for idx in range(len(items) - 1, -1, -1):
        it = items[idx]
        if it[0] == 'keyframes':
            key = (it[1], it[2])
            if key in seen_kf:
                dup_kf += 1
                continue
            seen_kf.add(key)
            keep[idx] = it
            continue
        if it[0] != 'rule':
            keep[idx] = it
            continue
        _, ctx, sels, decls = it
        kept = []
        for prop, val, imp in decls:
            overridden = all((ctx, s, prop) in later and (later[(ctx, s, prop)] or not imp) for s in sels)
            if overridden:
                dead += 1
            else:
                kept.append((prop, val, imp))
        for prop, _, imp in decls:
            for s in sels:
                later[(ctx, s, prop)] = later.get((ctx, s, prop), False) or imp
        if not kept:
            empty += 1
            continue
        signature = (ctx, tuple(sels), tuple(kept))
        if signature in seen_rules:
            dup += 1
            continue
        seen_rules.add(signature)
        keep[idx] = ('rule', ctx, sels, kept)
    return [k for k in keep if k is not None], dead, dup, empty, dup_kf


def main(argv):
    fix = '--fix' in argv
    problems = []
    for name, pages in BUNDLES.items():
        path = STYLE / f'{name}.css'
        if not path.is_file():
            problems.append(f'缺少样式包 样式/{name}.css')
            continue
        items = parse(path.read_text(encoding='utf-8'))
        kept, dead, dup, empty, dup_kf = analyse(items)
        if dead or dup or empty or dup_kf:
            if fix:
                header = re.match(r'\s*/\*.*?\*/', path.read_text(encoding='utf-8'), re.S)
                path.write_text((header.group(0) + '\n\n' if header else '') + serialize(kept), encoding='utf-8')
                print(f'FIXED {name}.css: 删除死声明 {dead}、重复规则 {dup}、空规则 {empty}、重复 keyframes {dup_kf}')
            else:
                problems.append(f'{name}.css: 死声明 {dead}、重复规则 {dup}、空规则 {empty}、重复 keyframes {dup_kf}（运行 python 工具/css_audit.py --fix）')
        for page in pages:
            html = (ROOT / f'{page}.html').read_text(encoding='utf-8')
            links = re.findall(r'<link rel="stylesheet" href="样式/([^"]+)"', html)
            if links != [f'{name}.css']:
                problems.append(f'{page}.html 应只加载 样式/{name}.css，实际为 {links}')
    if problems:
        print('FAIL: 样式审计\n  ' + '\n  '.join(problems))
        return 1
    if not fix:
        print('PASS: 样式审计（每页仅一个样式包；无死声明、重复规则与重复 keyframes）')
    return 0


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))
