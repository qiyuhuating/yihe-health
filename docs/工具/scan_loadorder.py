#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""scan_loadorder.py — 打印每个页面实际加载的 CSS / JS 顺序。

用途：文档/architecture.md 的加载顺序表必须与代码一致，手写容易失准；
本脚本导出 HTML 中真实的 script/link 顺序，供架构文档与代码复核使用。改动 HTML 的
script/link 标签后请重跑并同步文档。
"""
import pathlib
import re

ROOT = pathlib.Path(__file__).resolve().parents[1]
SKIP_DIRS = {".git", "_backup", "_shots", "__pycache__", ".venv", "test-results", "工具", "测试"}

SCRIPT_RE = re.compile(r'<script[^>]*\bsrc="([^"]+)"')
CSS_RE = re.compile(r'<link[^>]*\brel="stylesheet"[^>]*\bhref="([^"]+)"')
CSS_RE_ALT = re.compile(r'<link[^>]*\bhref="([^"]+\.css)"[^>]*\brel="stylesheet"')

# 有业务脚本的页面（内容页只有 脚本/site.js 与 脚本/theme.js，单独归类）
APP_PAGES = ["index.html", "管理端.html", "login.html", "design-preview.html"]
CONTENT_PAGES = ["home.html", "about.html", "services.html", "guide.html",
                 "news.html", "news-detail.html", "contact.html", "privacy.html", "yangheng.html"]


def refs(path, pattern_a, pattern_b=None):
    text = path.read_text(encoding="utf-8")
    out = []
    for pattern in (pattern_a, pattern_b):
        if pattern:
            out.extend(pattern.findall(text))
    return out


def show(title, pages):
    print(f"\n########## {title} ##########")
    for name in pages:
        path = ROOT / name
        if not path.exists():
            continue
        css = refs(path, CSS_RE, CSS_RE_ALT)
        js = refs(path, SCRIPT_RE)
        print(f"\n--- {name}")
        print(f"    CSS ({len(css)}): {', '.join(css) if css else '-'}")
        print(f"    JS  ({len(js)}):")
        for i, item in enumerate(js, 1):
            print(f"      {i:2d}. {item}")


def main():
    show("应用端（居民端 / 管理端 / 登录 / 设计校对）", APP_PAGES)
    show("内容页与官网", CONTENT_PAGES)

    # 汇总：哪些 js 从未被任何页面加载
    loaded = set()
    for path in ROOT.glob("*.html"):
        loaded.update(refs(path, SCRIPT_RE))
    all_js = {p.relative_to(ROOT).as_posix() for p in (ROOT / "脚本").glob("*.js")}
    orphans = sorted(all_js - loaded)
    print(f"\n########## 未被任何页面引用的 JS ({len(orphans)}) ##########")
    for item in orphans:
        print(f"  {item}")
    if not orphans:
        print("  （无）")


if __name__ == "__main__":
    main()
