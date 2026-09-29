#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""check_docs.py — 校验文档里"静态检查数字"与实际文件数一致。

要解决的问题
------------
仓库里三份文档曾同时写着 "45 个 JavaScript 文件、13 个 HTML 页面"、
"47 个 JS、13 个 HTML"、"45 个 JS、13 个 HTML"，而实际是 48/14。
根因：`测试/check_static.py` 动态打印真实数量，文档却手抄数字，
代码一变就失准，而且**没有任何机制会发现**。

对一个把"文档即交付契约"当方法论的仓库，契约自身失准是自相矛盾的。
本脚本把这个"发现机制"补上。

范围
----
默认校验根目录 README 和 文档/ 下的活文档。文档/archive/ 保存历史材料，
不按当前源码数字复核。带日期的文档也会自动跳过，因为它们记录的是某一时点。
文档/archive/ 以外新增到 文档/ 的 Markdown 文档默认纳入检查。

用法
----
    python 工具/check_docs.py            # 校验当前交付文档
    python 工具/check_docs.py --all      # 连带历史文档一起查（可能包含过时数据）
"""
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]

# 文件名含日期 -> 视为历史记录，跳过
DATED = re.compile(r"20\d{2}-?\d{2}-?\d{2}")

# 数字 -> 期望值
COUNT_PATTERNS = [
    (re.compile(r"(\d+)\s*个\s*JavaScript\s*文件"), "js"),
    (re.compile(r"(\d+)\s*个\s*JS(?!\w)"), "js"),
    (re.compile(r"(\d+)\s*个\s*HTML(?:\s*页面)?"), "html"),
    (re.compile(r"(\d+)\s*项\s*(?:Chromium\s*)?自动化回归"), "tests"),
    (re.compile(r"完整回归由\s*\d+\s*项增至\s*(\d+)\s*项"), "tests"),
]


def actual_counts():
    js = len(list((ROOT / "脚本").glob("*.js"))) \
        + len(list((ROOT / "数据").glob("*.js"))) + len(list((ROOT / "工具").glob("*.js")))
    html = len(list(ROOT.glob("*.html")))
    tests = 0
    for path in (ROOT / "测试").glob("test_*.py"):
        tests += len(re.findall(r"^\s*def test_", path.read_text(encoding="utf-8"), re.M))
    return {"js": js, "html": html, "tests": tests}


def main():
    expected = actual_counts()
    all_docs = sorted([ROOT / "README.md", *(ROOT / "文档").rglob("*.md")])
    if "--all" in sys.argv:
        docs = all_docs
        skipped = []
    else:
        docs = [p for p in all_docs if "archive" not in p.relative_to(ROOT).parts and not DATED.search(p.name)]
        skipped = [p for p in all_docs if p not in docs]

    print(f"实际: {expected['js']} 个 JS, {expected['html']} 个 HTML, "
          f"{expected['tests']} 个测试用例")
    print(f"校验 {len(docs)} 份活文档"
          + (f"，跳过 {len(skipped)} 份带日期的历史记录" if skipped else ""))
    print()

    errors = []
    for path in docs:
        text = path.read_text(encoding="utf-8")
        checked = 0
        for pattern, kind in COUNT_PATTERNS:
            for match in pattern.finditer(text):
                lineno = text[: match.start()].count("\n") + 1
                found = int(match.group(1))
                checked += 1
                if found != expected[kind]:
                    line = text.splitlines()[lineno - 1].strip()
                    errors.append(
                    f"{path.relative_to(ROOT)}:{lineno}: 文档写 {found}，实际 {expected[kind]}"
                        f"（{kind}）\n      {line[:100]}"
                    )
        if checked:
            print(f"  [OK] {path.relative_to(ROOT)}: {checked} 处数字与实际一致")
        else:
            print(f"  --  {path.relative_to(ROOT)}: 未出现可校验的数字")

    if errors:
        print("\n文档数字失准：")
        for e in errors:
            print("  " + e)
        print("\n提示：修正文档，或改写为引用命令输出而非硬编码，例如：")
        print('      `python 测试/check_static.py` 的输出即为权威数字。')
        raise SystemExit(1)
    print("\nPASS: 文档数字与实际一致")


if __name__ == "__main__":
    main()
