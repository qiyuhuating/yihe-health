#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# 用途：用快照、格式化幂等性和结构计数三项检查验证 CSS 变更未改动语义。
"""
check_css.py — CSS 反混淆的等价性校验（三重证明，任一不过即失败退出码 1）

证明 1（独立语义证明）
    原始文件与当前文件的**非空白 token 序列必须完全一致**。
    token 化时把字符串 / 注释 / url() 整体视为单个 token。
    因为 beautify 只插入空白、从不改动非空白字符，
    该条件成立即等价于"没有改动任何选择器、属性名、属性值"。

证明 2（规范形式证明）
    beautify(原始) 必须逐字节等于当前文件。
    即当前文件正好是原始文件的规范格式化结果，没有手工夹带改动。

证明 3（结构不变量证明）
    花括号数、圆括号数、@media 数、引号配平在两版本间必须一致。

原始快照位置：工具/css-baseline/<同名文件>（**已入库**，CI 需要它）
快照缺失时降级为"仅跑证明 3 + 幂等性检查"，并在输出中明确标注未做证明 1/2。
"""
import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
from beautify_css import beautify, scan_protected  # noqa: E402

ROOT = pathlib.Path(__file__).resolve().parents[1]

# 基线快照**必须入库**（放在 工具/css-baseline/），否则 CI 上跑不到证明 1/2。
# 放在 _backup/ 下是不行的 —— 那个目录被 .gitignore 排除，CI 上不存在，
# 校验会静默降级成"只查括号配平"，等于最强的那条证明在 CI 上从未真正执行过。
SNAPSHOT = ROOT / "工具" / "css-baseline"

# Active pages use the rebuilt reconstruction.css.
# design-preview.css and tokens.css support the isolated design preview.
TARGETS = [
    "样式/reconstruction.css",
    "样式/community.css",
    "样式/design-preview.css",
    "样式/tokens.css",
]

PROTECTED_STRING = "string"
PROTECTED_COMMENT = "comment"
PROTECTED_URL = "url"


def canonical(text):
    """把 CSS 规范化成"删除保护区域外全部空白"的字符串。

    beautify 的唯一作用就是增删保护区域之外的空白，因此两个版本的
    canonical 形式必须**逐字节相同**。这是"未改动任何选择器、属性名、
    属性值"的最强且最简单的证明（比逐 token 比对更强：连 token 的切分
    方式都不同也不影响，因为切分只由空白决定）。
    """
    mask = scan_protected(text)
    return "".join(ch for ch, m in zip(text, mask) if m is None and not ch.isspace())


def structure(text):
    mask = scan_protected(text)
    plain = "".join(ch for ch, m in zip(text, mask) if m is None)
    return {
        "braces": (plain.count("{"), plain.count("}")),
        "parens": (plain.count("("), plain.count(")")),
        "media": plain.count("@media"),
        "supports": plain.count("@supports"),
        "keyframes": plain.count("@keyframes"),
    }


def main():
    failures = []
    for rel in TARGETS:
        path = ROOT / rel
        if not path.exists():
            failures.append(f"{rel}: 文件不存在")
            continue
        current = path.read_text(encoding="utf-8")
        snap = SNAPSHOT / pathlib.Path(rel).name

        if not snap.exists():
            # 降级：只验证幂等性与结构自洽
            if beautify(current) != current:
                failures.append(f"{rel}: 尚未格式化（beautify 不再是恒等变换）")
            st = structure(current)
            if st["braces"][0] != st["braces"][1] or st["parens"][0] != st["parens"][1]:
                failures.append(f"{rel}: 括号不配平 {st}")
            print(f"  ~ {rel}: 无原始快照，仅校验幂等性与结构自洽")
            continue

        original = snap.read_text(encoding="utf-8")

        # 证明 1
        co, cc = canonical(original), canonical(current)
        if co != cc:
            failures.append(
                f"{rel}: 证明1失败 —— canonical 形式不一致（原 {len(co)} 字符 / 现 {len(cc)} 字符）"
            )
            for k, (a, b) in enumerate(zip(co, cc)):
                if a != b:
                    lo = max(0, k - 40)
                    failures.append(f"    首个差异 @{k}: 原 ...{co[lo:k + 40]!r}... vs 现 ...{cc[lo:k + 40]!r}...")
                    break
        else:
            print(f"  [OK] {rel}: 证明1通过（canonical {len(cc)} 字符逐字节一致）")

        # 证明 2
        if beautify(original) != current:
            failures.append(f"{rel}: 证明2失败 —— 当前文件不是原始文件的规范格式化结果")
        else:
            print(f"  [OK] {rel}: 证明2通过（等于 beautify(原始)）")

        # 证明 3
        so, sc = structure(original), structure(current)
        if so != sc:
            failures.append(f"{rel}: 证明3失败 —— 结构不变量变化 {so} -> {sc}")
        else:
            print(f"  [OK] {rel}: 证明3通过（结构不变量一致 {sc['braces'][0]} 条规则 / {sc['media']} 处 @media）")

    if failures:
        print("\nCSS 等价性校验失败：")
        for f in failures:
            print("  " + f)
        raise SystemExit(1)
    print("\nPASS: CSS 反混淆等价性校验全部通过")


if __name__ == "__main__":
    main()
