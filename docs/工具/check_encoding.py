#!/usr/bin/env python3
"""Reject invalid UTF-8, replacement characters, and known mojibake markers."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
TEXT_SUFFIXES = {
    ".cjs", ".css", ".html", ".js", ".json", ".md", ".mjs", ".py",
    ".ps1", ".sh", ".svg", ".toml", ".txt", ".xml", ".yaml", ".yml",
}
SKIP_DIRS = {".git", ".venv", "__pycache__", "dist", "node_modules", "test-results"}
MOJIBAKE_MARKERS = (
    "\ufffd",
    "\u9225?",
    "\u951f\u65a4\u62f7",
    "\u00ef\u00bf\u00bd",
    "\u00c3\u00a9",
    "\u00c3\u00a4",
    "\u00e2\u20ac\u2122",
    "\u68f0\u610c\u62f0",
)


def main():
    failures = []
    for path in ROOT.rglob("*"):
        if not path.is_file() or path.suffix.lower() not in TEXT_SUFFIXES:
            continue
        if any(part in SKIP_DIRS or part == "archive" for part in path.relative_to(ROOT).parts):
            continue
        try:
            text = path.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            relative = path.relative_to(ROOT).as_posix().encode("unicode_escape").decode("ascii")
            failures.append(f"{relative}: invalid UTF-8")
            continue
        for marker in MOJIBAKE_MARKERS:
            if marker in text:
                relative = path.relative_to(ROOT).as_posix().encode("unicode_escape").decode("ascii")
                escaped_marker = marker.encode("unicode_escape").decode("ascii")
                failures.append(f"{relative}: suspicious encoding marker {escaped_marker}")

    if failures:
        for failure in failures:
            print(f"[FAIL] {failure}")
        return 1
    print("[OK] active frontend text files are UTF-8 without known mojibake markers")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
