#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""check_docs.py — 校验文档里"静态检查数字"与实际文件数一致。

要解决的问题
------------
仓库里三份文档曾同时写着 "45 个 JavaScript 文件、13 个 HTML 页面"、
"47 个 JS、13 个 HTML"、"45 个 JS、13 个 HTML"，而实际是 48/14。
根因：`tests/check_static.py` 动态打印真实数量，文档却手抄数字，
代码一变就失准，而且**没有任何机制会发现**。

对一个把"文档即交付契约"当方法论的仓库，契约自身失准是自相矛盾的。
本脚本把这个"发现机制"补上。

范围
----
默认校验**不带日期**的文档（README / FRONTEND-HANDOFF / IMPLEMENTATION
以及今后新增的任何活文档），因为它们描述"当前状态"。

带日期的文档（文件名含 20xx-xx-xx 或 20xxxxxxxx，如
`缺陷修复报告_20260925.md`、`优化计划-20260926.md`）自动跳过：
它们是**某一时点的历史记录**，本来就该与现在不同。强改它们等于篡改历史。

之所以用"排除带日期的"而不是"白名单活文档"：白名单需要有人记得把新文档
加进去，漏加就静默失守；排除规则对新文档默认生效，不用维护。

用法
----
    python tools/check_docs.py            # 校验全部无日期文档
    python tools/check_docs.py --all      # 连带日期的历史报告一起查（预期失败，仅供了解差异）
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
    js = len(list(ROOT.glob("*.js"))) + len(list((ROOT / "js").glob("*.js"))) \
        + len(list((ROOT / "data").glob("*.js"))) + len(list((ROOT / "tools").glob("*.js")))
    html = len(list(ROOT.glob("*.html")))
    tests = 0
    for path in (ROOT / "tests").glob("test_*.py"):
        tests += len(re.findall(r"^\s*def test_", path.read_text(encoding="utf-8"), re.M))
    return {"js": js, "html": html, "tests": tests}


def main():
    expected = actual_counts()
    all_docs = sorted(ROOT.glob("*.md"))
    if "--all" in sys.argv:
        docs = all_docs
        skipped = []
    else:
        docs = [p for p in all_docs if not DATED.search(p.name)]
        skipped = [p.name for p in all_docs if DATED.search(p.name)]

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
                        f"{path.name}:{lineno}: 文档写 {found}，实际 {expected[kind]}"
                        f"（{kind}）\n      {line[:100]}"
                    )
        if checked:
            print(f"  [OK] {path.name}: {checked} 处数字与实际一致")
        else:
            print(f"  --  {path.name}: 未出现可校验的数字")

    if errors:
        print("\n文档数字失准：")
        for e in errors:
            print("  " + e)
        print("\n提示：修正文档，或改写为引用命令输出而非硬编码，例如：")
        print('      `python tests/check_static.py` 的输出即为权威数字。')
        raise SystemExit(1)
    print("\nPASS: 文档数字与实际一致")


if __name__ == "__main__":
    main()
