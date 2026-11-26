"""Verify links in changed Markdown files in a docs-only pull request."""

from __future__ import annotations

import os
from pathlib import Path
import re
import sys
from urllib.parse import unquote, urlsplit

from pr_change_scope import changed_paths, docs_only


_LINK = re.compile(r"!?(?:\[[^\]]*\])\(([^)]+)\)")


def check_file(root: Path, relative: str) -> list[str]:
    file = root / relative
    if not file.is_file():
        return []  # Deleted documentation has no links to verify.
    errors = []
    for match in _LINK.finditer(file.read_text(encoding="utf-8")):
        target = match.group(1).split(maxsplit=1)[0].strip("<>")
        if not target or target.startswith("#"):
            continue
        parsed = urlsplit(target)
        if parsed.scheme or parsed.netloc:
            continue
        path = unquote(parsed.path)
        if not path:
            continue
        dest = ((root if path.startswith("/") else file.parent) / path.lstrip("/")).resolve()
        if not dest.is_relative_to(root) or not dest.exists():
            errors.append(f"{relative}: broken relative link {target}")
    return errors


def main() -> None:
    base = os.environ["BASE_SHA"]
    head = os.environ["HEAD_SHA"]
    paths = changed_paths(base, head)
    if not docs_only(paths):
        raise SystemExit("Docs check must only run for documentation-only PRs")
    root = Path.cwd().resolve()
    errors = [
        error
        for path in paths
        if path.endswith((".md", ".mdx", ".markdown"))
        for error in check_file(root, path)
    ]
    if errors:
        print("\n".join(errors), file=sys.stderr)
        raise SystemExit(1)
    print(f"Verified local links in {sum(p.endswith(('.md', '.mdx', '.markdown')) for p in paths)} changed documents")


if __name__ == "__main__":
    main()
