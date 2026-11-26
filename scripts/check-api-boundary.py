"""Fail closed if the shipping Go, Vue, Tauri or watch clients refer to removed HTTP routes.

Only actively shipped source, build scripts and tests are scanned. Historical
records and immutable CI evidence are not rewritten by this migration.
"""
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
OBSOLETE = ("rest" + "ful", "J" + "SESSIONID")
SUFFIXES = {".go", ".rs", ".js", ".cjs", ".mjs", ".ts", ".ets", ".vue",
            ".html", ".json", ".json5", ".yml", ".yaml", ".toml",
            ".py", ".ps1", ".sh", ".md", ".txt"}
HISTORY = ("docs/decisions/", "docs/releases/", "docs/performance/",
           "docs/refactoring/", "evidence/")
PREFIXES = ("README.md", "README.en.md", "docs/",
            "backend/",
            "frontend/",
            "desktop/",
            "watch-sim/",
            "YiheWatch/", ".github/workflows/", "deploy/", "tests/", "scripts/")
files = subprocess.check_output(["git", "ls-files", "-z"], cwd=ROOT).decode("utf-8").split("\0")
errors = []
if (ROOT / 'oldIot').exists():
    errors.append('historical frontend directory must not be restored')
module = (ROOT / 'backend/go.mod').read_text(encoding='utf-8')
if not module.splitlines() or module.splitlines()[0].strip() != 'module yihe-care':
    errors.append('backend must use the native yihe-care Go module')
for name in files:
    if not name or not name.startswith(PREFIXES) or name.startswith(HISTORY):
        continue
    if name.endswith(".lock") or "/public/static/" in name:
        continue
    path = ROOT / name
    if not path.exists() or path.suffix.lower() not in SUFFIXES:
        continue
    try:
        content = path.read_text(encoding="utf-8")
    except UnicodeDecodeError:
        continue
    for line, text in enumerate(content.splitlines(), 1):
        if any(word.casefold() in text.casefold() for word in OBSOLETE):
            errors.append(f"{name}:{line}: forbidden decommissioned API/session reference")
if errors:
    print("\n".join(errors[:150]), file=sys.stderr)
    raise SystemExit(f"Native Go API purity check failed: {len(errors)} stale references")
print(f"Native Go API purity check passed ({len(files)} tracked paths examined)")
