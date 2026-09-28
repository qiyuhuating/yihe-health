#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""scan_density.py — 按"平均行长"排序，找出真正的压缩/难读源文件。

用途：定位可维护性问题（压缩产物当源码提交）。

判定口径必须用字节级 splitlines 统计。**不要**用 PowerShell 的
`Get-Content -Raw` 再按 "`n" 切分：它对 CRLF 的处理会给出错误的行数
（本项目就因此把两个 2761 行的 CSS 误判成"单行 41489 字符"）。
"""
import pathlib
import statistics
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
# tools/ 也要跳过：其中的 tools/css-baseline/ 存的是**故意保持压缩形态**的
# 反混淆前基线快照，扫进来只会制造噪声。
SKIP_DIRS = {".git", "_backup", "_shots", "__pycache__", ".venv", "test-results", "tools"}
THRESHOLD = 60.0   # 平均行长超过此值视为"打包过密"


def main():
    rows = []
    for path in ROOT.rglob("*"):
        if not path.is_file():
            continue
        if any(part in SKIP_DIRS for part in path.parts):
            continue
        if path.suffix not in {".js", ".css"}:
            continue
        try:
            text = path.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            continue
        lines = text.splitlines()
        if not lines:
            continue
        rel = path.relative_to(ROOT).as_posix()
        mean = statistics.mean(len(l) for l in lines)
        rows.append((mean, max(len(l) for l in lines), len(lines), rel))

    rows.sort(reverse=True)
    print("  mean    max   lines  file")
    dense = [r for r in rows if r[0] > THRESHOLD]
    for mean, longest, count, rel in dense:
        print(f"{mean:6.1f} {longest:6d} {count:6d}  {rel}")
    print(f"\n共 {len(rows)} 个 JS/CSS 文件，其中 {len(dense)} 个平均行长 > {THRESHOLD:.0f} 字符")
    print("注意：平均行长小不代表可读 —— 还需检查标识符是否被混淆")
    print("（如 js/mock-api.js 平均行长不高，但全是单字母变量且多层遮蔽）。")
    if "--verbose" in sys.argv:
        print("\n全部文件：")
        for mean, longest, count, rel in sorted(rows, key=lambda r: r[3]):
            print(f"{mean:6.1f} {longest:6d} {count:6d}  {rel}")


if __name__ == "__main__":
    main()
