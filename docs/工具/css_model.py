"""无依赖的最小 CSS 解析 / 序列化（供 css_audit.py 使用）。只处理 @media 分组与 @keyframes，满足本项目样式。"""
import re

def strip_comments(text):
    out, i, n = [], 0, len(text)
    while i < n:
        c = text[i]
        if c in '"\'':
            j = i + 1
            while j < n and text[j] != c:
                j += 2 if text[j] == '\\' else 1
            out.append(text[i:j + 1]); i = j + 1
        elif text.startswith('/*', i):
            j = text.find('*/', i + 2); i = n if j < 0 else j + 2
        else:
            out.append(c); i += 1
    return ''.join(out)

def split_top(s, sep):
    parts, depth, cur, q = [], 0, [], None
    for ch in s:
        if q:
            cur.append(ch)
            if ch == q: q = None
            continue
        if ch in '"\'': q = ch; cur.append(ch); continue
        if ch in '([': depth += 1
        elif ch in ')]': depth -= 1
        if ch == sep and depth == 0:
            parts.append(''.join(cur)); cur = []
        else: cur.append(ch)
    parts.append(''.join(cur))
    return parts

def parse_block(text, i=0, ctx=()):
    """Return (items, next_index). items: ('rule', ctx, [selectors], [(prop,val,imp)]) | ('keyframes', name, raw) | ('raw', ctx, text)."""
    items, n = [], len(text)
    while i < n:
        while i < n and text[i].isspace(): i += 1
        if i >= n or text[i] == '}': return items, i
        j, depth, q = i, 0, None
        while j < n:
            ch = text[j]
            if q:
                if ch == q: q = None
            elif ch in '"\'': q = ch
            elif ch in '([': depth += 1
            elif ch in ')]': depth -= 1
            elif depth == 0 and ch in '{;}': break
            j += 1
        prelude = text[i:j].strip()
        if j >= n: break
        if text[j] == ';':
            items.append(('raw', ctx, prelude + ';')); i = j + 1; continue
        if text[j] == '}': return items, j
        # block
        if prelude.startswith('@'):
            name = prelude.split()[0].lower()
            if name in ('@media', '@supports', '@layer', '@container'):
                inner, k = parse_block(text, j + 1, ctx + (re.sub(r'\s+', ' ', prelude),))
                items.extend(inner); i = k + 1
            else:                                   # @keyframes / @font-face: keep raw, brace-matched
                depth2, k = 1, j + 1
                while k < n and depth2:
                    depth2 += (text[k] == '{') - (text[k] == '}'); k += 1
                body = text[j + 1:k - 1]
                if name in ('@keyframes', '@-webkit-keyframes'):
                    items.append(('keyframes', ctx, prelude, body))
                else:
                    items.append(('raw', ctx, prelude + '{' + body + '}'))
                i = k
        else:
            k = j + 1; depth, q = 0, None
            while k < n:
                ch = text[k]
                if q:
                    if ch == q: q = None
                elif ch in '"\'': q = ch
                elif ch in '([': depth += 1
                elif ch in ')]': depth -= 1
                elif ch == '}' and depth == 0: break
                k += 1
            decls = []
            for d in split_top(text[j + 1:k], ';'):
                d = d.strip()
                if not d or ':' not in d: continue
                prop, val = d.split(':', 1)
                val = val.strip(); imp = False
                m = re.search(r'\s*!\s*important\s*$', val, re.I)
                if m: imp = True; val = val[:m.start()].strip()
                decls.append((prop.strip(), val, imp))
            sels = [re.sub(r'\s+', ' ', s.strip()) for s in split_top(prelude, ',') if s.strip()]
            items.append(('rule', ctx, sels, decls)); i = k + 1
    return items, i

def parse(text):
    items, _ = parse_block(strip_comments(text))
    return items

def fmt_decl(d): return f'{d[0]}: {d[1]}{" !important" if d[2] else ""};'
def serialize(items, origin_marks=None):
    """items must be ('rule'|'keyframes'|'raw', ...). Adjacent items sharing a ctx are emitted in one @media block."""
    out, current = [], ()
    def open_ctx(ctx):
        for c in ctx: out.append(c + ' {')
    def close_ctx(ctx): out.extend('}' for _ in ctx)
    for idx, it in enumerate(items):
        ctx = it[1]
        if origin_marks and idx in origin_marks:
            close_ctx(current); current = ()
            out.append(''); out.append(f'/* ── {origin_marks[idx]} ── */')
        if ctx != current:
            close_ctx(current); open_ctx(ctx); current = ctx
        ind = '  ' * len(ctx)
        if it[0] == 'rule':
            sels = ',\n'.join(ind + s for s in it[2])
            body = '\n'.join(ind + '  ' + fmt_decl(d) for d in it[3])
            out.append(f'{sels} {{\n{body}\n{ind}}}')
        elif it[0] == 'keyframes':
            out.append(f'{ind}{it[2]} {{{it[3].rstrip()}}}')
        else:
            out.append(ind + it[2])
    close_ctx(current)
    return '\n'.join(out) + '\n'
