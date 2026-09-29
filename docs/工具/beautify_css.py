#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
beautify_css.py — 把压缩成"每行塞满"的 CSS 重排为可读缩进格式。

设计约束（安全性的全部依据）
============================
CSS 里空白是**有语义**的：
    margin:0 auto   ≠  margin:0auto
    a b (后代选择器)  ≠  ab (标签名)
所以本工具绝不做"整体重排"，只在**四个结构点**规范换行。

四个结构点（只在这些位置把后续空白run 规范成"换行 + 缩进"）：
    1. `{` 之后   -> 换行 + (depth+1) 级缩进
    2. `}` 之前   -> 换行 + (depth-1) 级缩进
    3. `;` 之后   -> 换行 + depth 级缩进（且 depth>0，即声明块内）
    4. `,` 之后   -> 换行 + 0 级缩进（且 depth==0 且 paren==0，即选择器上下文）

为什么"把空白run 换成换行"是安全的
----------------------------------
CSS 中一段连续空白只可能是两种角色：分隔符，或后代选择器的空格。
换行同样是空白，因此 `a<空白>b` 与 `a<换行>b` 语义完全相同。
字符串 / 注释 / url() 内部的空白是**有意义**的，因此全部列入保护区域，
一个字符都不动。

非结构点的空白run 原样保留 —— 这样对已经格式化的文件，本工具是恒等变换
（可直接用于校验"这个文件是否已符合规范"，而不必再存一份原始快照）。

保护区域（完全不改动）：
    - 字符串 '...' 与 "..."
    - 注释 /* ... */
    - url(...) 的未加引号内容（内含 base64 / data URI 里的 , ; { }）
    - 圆括号内部（:is(a, b)、@media (a),(b)、transform(...)）

`check_css.py` 会独立验证：非空白 token 序列必须与原文件**完全一致**。
因为本工具从不增删任何非空白字符，该条件是"未改动语义"的充分证明。
"""
import pathlib
import sys

PROTECTED_STRING = "string"
PROTECTED_COMMENT = "comment"
PROTECTED_URL = "url"


def scan_protected(text):
    """返回与 text 等长的掩码数组，元素为 None 或保护类型标记。"""
    mask = [None] * len(text)
    i, n = 0, len(text)
    while i < n:
        ch = text[i]
        # 注释
        if ch == "/" and i + 1 < n and text[i + 1] == "*":
            end = text.find("*/", i + 2)
            end = n if end < 0 else end + 2
            for k in range(i, end):
                mask[k] = PROTECTED_COMMENT
            i = end
            continue
        # 字符串
        if ch in "'\"":
            j = i + 1
            while j < n:
                if text[j] == "\\":
                    j += 2
                    continue
                if text[j] == ch:
                    j += 1
                    break
                j += 1
            for k in range(i, min(j, n)):
                mask[k] = PROTECTED_STRING
            i = min(j, n)
            continue
        # url( 未加引号内容
        if ch in "uU" and text.startswith("url(", i) and (i == 0 or not _is_ident(text[i - 1])):
            j = i + 4
            while j < n and text[j] != ")":
                j += 1
            for k in range(i, min(j + 1, n)):
                mask[k] = PROTECTED_URL
            i = min(j + 1, n)
            continue
        i += 1
    return mask


def _is_ident(ch):
    return ch.isalnum() or ch in "-_\\"


def beautify(text, indent="  "):
    if not text.strip():
        return text
    mask = scan_protected(text)
    out = []
    depth = 0          # 花括号深度
    paren = 0          # 圆括号深度
    n = len(text)
    i = 0
    force_nl = None    # 待施加的换行缩进层级；None 表示不强制

    while i < n:
        ch = text[i]
        prot = mask[i]

        # --- 1. 上一字符是结构点：先把待施加的换行落地 ---
        #     必须放在受保护区域处理之前，否则紧跟 { ; , } 的注释
        #     会被吸到结构字符同一行上（例如 ":root{" 后面直接跟 /* ... */）。
        if force_nl is not None:
            level = force_nl
            force_nl = None
            if prot is None and ch.isspace():
                k = i
                while k < n and text[k].isspace() and mask[k] is None:
                    k += 1
                out.append("\n" + indent * level)
                i = k
                continue
            out.append("\n" + indent * level)

        # --- 2. 受保护区域：原样输出，一个字符都不动 ---
        if prot:
            if prot == PROTECTED_URL:
                j = text.find(")", i)
                j = n if j < 0 else j + 1
            elif prot == PROTECTED_COMMENT:
                e = text.find("*/", i)
                j = n if e < 0 else e + 2
                # 注释自身不改一个字符，但强制其后换行，避免注释"吸走"
                # 紧随其后的声明（否则会出现 "/* 说明 */--cm-1:...;" 这种行）。
                # 注释在 CSS 里只是 token 分隔符，其后换行不改变任何语义。
                force_nl = depth
            else:  # string
                q = text[i]
                j = i + 1
                while j < n:
                    if text[j] == "\\":
                        j += 2
                        continue
                    if text[j] == q:
                        j += 1
                        break
                    j += 1
            out.append(text[i:j])
            i = j
            continue

        # --- 3. 非结构点的空白：原样保留 ---
        if ch.isspace():
            k = i
            while k < n and text[k].isspace() and mask[k] is None:
                k += 1
            out.append(text[i:k])
            i = k
            continue

        # --- 4. 结构字符 ---
        if ch == "{":
            out.append(ch)
            depth += 1
            force_nl = depth
            i += 1
            continue

        if ch == "}":
            depth -= 1
            if depth < 0:
                raise SystemExit("beautify: 花括号不配平（提前出现 '}'）")
            force_nl = depth
            out.append(ch)
            i += 1
            continue

        if ch == ";":
            out.append(ch)
            if depth > 0:
                force_nl = depth
            i += 1
            continue

        if ch == ",":
            out.append(ch)
            if depth == 0 and paren == 0:
                force_nl = 0
            i += 1
            continue

        if ch == "(":
            paren += 1
        elif ch == ")":
            paren -= 1

        out.append(ch)
        i += 1

    if depth != 0:
        raise SystemExit(f"beautify: 花括号不配平（结束时 depth={depth}）")
    if paren != 0:
        raise SystemExit(f"beautify: 圆括号不配平（结束时 paren={paren}）")

    lines = [ln.rstrip() for ln in "".join(out).split("\n")]
    while lines and not lines[0]:
        lines.pop(0)
    while lines and not lines[-1]:
        lines.pop()
    return "\n".join(lines) + "\n"


def main():
    if len(sys.argv) < 2:
        raise SystemExit("用法: python 工具/beautify_css.py <file.css> [...]")
    for arg in sys.argv[1:]:
        path = pathlib.Path(arg)
        original = path.read_text(encoding="utf-8")
        result = beautify(original)
        if result == original:
            print(f"  已格式化（无变化）: {path.name}")
            continue
        path.write_text(result, encoding="utf-8", newline="\n")
        before = len(original.splitlines())
        after = len(result.splitlines())
        print(f"  {path.name}: {before} 行 -> {after} 行")


if __name__ == "__main__":
    main()
