"""静态检查：JS 语法、HTML 重复 ID、失效本地引用、脚本加载顺序、innerHTML 转义。

新增的检查（2026-09-26）
-------------------------
1. **脚本加载顺序**：凡读取 脚本/config.js 的模块，其所在页面必须先加载 config.js。
   凭据集中到 config.js 之后，漏加载会让演示登录全线失效，而语法检查发现不了。
2. **innerHTML 转义**：含模板插值的 innerHTML 赋值必须同处出现 escapeHtml。
   项目原本 34 处 innerHTML 全部转义，这个优点此前只靠人工保持。

已知局限：第 2 项是**逐行**判断。跨行的 innerHTML + 模板字面量组合检测不到。
"""
import pathlib
import re
import subprocess
from collections import Counter
from html.parser import HTMLParser
from urllib.parse import unquote, urlsplit

ROOT = pathlib.Path(__file__).resolve().parents[1]
errors = []
warnings = []

# 读取 YIHE_CONFIG 的模块 -> 所在页面必须先加载 脚本/config.js
CONFIG_CONSUMERS = {
    "脚本/admin-credentials.js",
    "脚本/admin-login.js",
    "脚本/login-page.js",
    "脚本/login.js",
    "脚本/ai-chat.js",
}
CONFIG_SRC = "脚本/config.js"


class Page(HTMLParser):
    def __init__(self):
        super().__init__()
        self.ids = []
        self.refs = []
        self.scripts = []

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if attrs.get("id"):
            self.ids.append(attrs["id"])
        if tag == "script" and attrs.get("src"):
            self.scripts.append(attrs["src"])
        for key in ("src", "href"):
            if attrs.get(key):
                self.refs.append(attrs[key])


# ---------- 1. JS 语法 ----------
js_files = (
    list((ROOT / "脚本").glob("*.js"))
    + list((ROOT / "数据").glob("*.js"))
    + list((ROOT / "工具").glob("*.js"))
)
for path in js_files:
    result = subprocess.run(["node", "--check", str(path)], capture_output=True, text=True)
    if result.returncode:
        errors.append(result.stderr)

# ---------- 2. HTML：重复 ID、失效引用、脚本顺序 ----------
html_files = list(ROOT.glob("*.html"))
for path in html_files:
    page = Page()
    page.feed(path.read_text(encoding="utf-8-sig"))
    errors.extend(f"{path.name}: duplicate id {key}" for key, n in Counter(page.ids).items() if n > 1)
    for ref in page.refs:
        parsed = urlsplit(ref)
        if parsed.scheme or parsed.netloc or not parsed.path:
            continue
        if not (path.parent / unquote(parsed.path)).exists():
            errors.append(f"{path.name}: missing {ref}")

    scripts = [s for s in page.scripts if not urlsplit(s).scheme]
    if not scripts:
        continue
    dependencies = {
        '脚本/care-transport.js': ['脚本/care-runtime-config.js'],
        '脚本/care-session.js': ['脚本/care-transport.js'],
        '脚本/admin-login.js': ['脚本/care-session.js'],
        '脚本/login-page.js': ['脚本/care-session.js'],
        '脚本/care-http-adapter.js': ['脚本/care-transport.js'],
        '脚本/care-api.js': ['脚本/care-runtime-config.js', '脚本/care-http-adapter.js'],
        '脚本/staff-care.js': ['脚本/care-api.js', '脚本/care-ui.js', '脚本/admin-login.js'],
        '脚本/resident-care.js': ['脚本/care-api.js', '脚本/care-ui.js', '脚本/care-session.js'],
    }
    for consumer, requirements in dependencies.items():
        if consumer in scripts:
            for required in requirements:
                if required not in scripts or scripts.index(required) > scripts.index(consumer):
                    errors.append(f'{path.name}: {required} must precede {consumer}')
    for consumer in CONFIG_CONSUMERS:
        if consumer in scripts:
            if CONFIG_SRC not in scripts:
                errors.append(f"{path.name}: 加载了 {consumer} 但没有加载 {CONFIG_SRC}")
            elif scripts.index(CONFIG_SRC) > scripts.index(consumer):
                errors.append(f"{path.name}: {CONFIG_SRC} 必须在 {consumer} 之前加载")

# ---------- 3. innerHTML 转义（锁定项目已有的好实践）----------
INNER_HTML_TMPL = re.compile(r"\.innerHTML\s*=")
INTERP = re.compile(r"\$\{")
for path in js_files + [ROOT / "脚本/site.js", ROOT / "news-detail.js", ROOT / "脚本/theme.js"]:
    if not path.exists():
        continue
    for lineno, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        if INNER_HTML_TMPL.search(line) and INTERP.search(line) and "escapeHtml" not in line:
            errors.append(
                f"{path.relative_to(ROOT)}:{lineno}: innerHTML 含模板插值但未见 escapeHtml"
                f"（逐行判断，跨行写法可能漏检）"
            )

# ---------- 4. console.log 残留：只报告，不阻断 ----------
# 只统计一手应用代码；工具/ 是命令行工具（本来就要打印），
# *.min.js 是第三方压缩产物（不重排、不手改），两者都不计入。
console_log = []
app_js = [p for p in js_files if p.parent.name == "脚本" and p.suffix == ".js"]
app_js = [p for p in app_js if not p.name.endswith(".min.js")]
app_js += [ROOT / "脚本/site.js", ROOT / "news-detail.js", ROOT / "脚本/theme.js"]
for path in app_js:
    if not path.exists():
        continue
    for lineno, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        if re.search(r"console\.(log|debug)\s*\(", line):
            console_log.append(f"  {path.relative_to(ROOT)}:{lineno}")
if console_log:
    warnings.append(
        f"console.log/debug 残留 {len(console_log)} 处（仅一手代码，不阻断）：\n" + "\n".join(console_log)
    )

# ---------- 汇总 ----------
for w in warnings:
    print("WARN: " + w)
if errors:
    raise SystemExit("\n".join(errors))
print(f"PASS: {len(js_files)} JavaScript files, {len(html_files)} HTML pages, "
      f"script-order {len(CONFIG_CONSUMERS)} consumers, innerHTML escaping checked")
