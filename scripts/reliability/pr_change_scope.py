"""Choose CI jobs by changed paths; unknown changes fail closed to full CI."""

from __future__ import annotations

import json
import os
from pathlib import PurePosixPath
import re
import subprocess

_SHA = re.compile(r"[0-9a-f]{40}\Z")
JOBS = ("fast_gate", "frontend", "ui", "backend", "watch", "simulator",
        "windows", "desktop", "deployment", "security_scan", "dependency",
        "codeql_go", "codeql_js", "ci_tests")


def is_documentation(path: str) -> bool:
    if path.startswith(".github/ISSUE_TEMPLATE/"):
        return PurePosixPath(path).suffix.lower() in {".yml", ".yaml"}
    return PurePosixPath(path).suffix.lower() in {".md", ".mdx", ".markdown"}


def docs_only(paths: list[str]) -> bool:
    return bool(paths) and all(is_documentation(path) for path in paths)


def classify(paths: list[str]) -> dict[str, bool]:
    flags = {name: False for name in JOBS}
    flags["docs_only"] = docs_only(paths)
    if flags["docs_only"]:
        return flags
    if not paths or any(path.startswith((".github/workflows/", "scripts/reliability/"))
                        or path == ".nvmrc" for path in paths):
        return {key: True for key in flags if key != "docs_only"} | {"docs_only": False}

    for path in paths:
        if is_documentation(path):
            continue
        if path.startswith("frontend/"):
            flags.update(fast_gate=True, frontend=True, codeql_js=True)
            if path.startswith(("frontend/src/", "frontend/public/", "frontend/qa/")) or path.endswith((".json", ".ts", ".mjs", ".vue")):
                flags["ui"] = True
        elif path.startswith("backend/"):
            flags.update(fast_gate=True, backend=True, windows=True,
                         security_scan=True, codeql_go=True)
            if path.startswith(("backend/internal/apidoc/",)):
                flags["frontend"] = True
            if path.startswith((
                "backend/cmd/server/", "backend/internal/devicerepo/",
                "backend/internal/telemetryrepo/", "backend/internal/telemetryworkflow/",
                "backend/internal/health/", "backend/internal/healthalertstate/",
                "backend/internal/locationalertstate/", "backend/internal/command",
                "backend/internal/handlers/handler_device",
                "backend/internal/handlers/handler_vitals",
                "backend/internal/handlers/handler_fences",
            )):
                flags.update(watch=True, simulator=True)
        elif path.startswith("YiheWatch/"):
            flags.update(watch=True, simulator=True)
        elif path.startswith("desktop/"):
            flags.update(windows=True, desktop=True)
        elif path.startswith("deploy/") or path == "scripts/start-platform.py":
            flags["deployment"] = True
        elif path.startswith("tests/ui/") or path == "scripts/visual-rmse.mjs" or path.startswith("docs/screenshots/"):
            flags.update(frontend=True, ui=True)
        elif path in ("scripts/check-api-boundary.py", "scripts/check-go-module-boundaries.py"):
            flags.update(fast_gate=True, backend=True)
        elif path == "scripts/generate-openapi.py":
            flags.update(fast_gate=True, backend=True, frontend=True)
        elif path == ".github/dependabot.yml":
            flags["dependency"] = True
        else:
            # New configuration or previously unclassified build paths cannot silently skip checks.
            return {key: True for key in flags if key != "docs_only"} | {"docs_only": False}
        if path.endswith((
            "package.json", "package-lock.json", "go.mod", "go.sum",
            "Cargo.toml", "Cargo.lock", "oh-package.json5", "oh-package-lock.json5",
        )):
            flags["dependency"] = True
    return flags


def changed_paths(base: str, head: str) -> list[str]:
    if not _SHA.fullmatch(base) or not _SHA.fullmatch(head):
        raise ValueError("CI base and head must be full commit SHAs")
    result = subprocess.run(
        ["git", "diff", "--name-only", "--no-renames", "-z", base, head, "--"],
        check=True, capture_output=True,
    )
    return [os.fsdecode(p) for p in result.stdout.split(b"\0") if p]


def main() -> None:
    base = os.environ.get("BASE_SHA", "")
    head = os.environ.get("HEAD_SHA", "")
    # A manual dispatch or an unknown/first-push base deliberately runs everything.
    paths = changed_paths(base, head) if _SHA.fullmatch(base) and base != "0" * 40 else []
    flags = classify(paths)
    for name, enabled in flags.items():
        print(f"{name}={'true' if enabled else 'false'}")
    matrix = []
    if flags["codeql_go"]:
        matrix.append({"language": "go", "build-mode": "manual"})
    if flags["codeql_js"]:
        matrix.append({"language": "javascript-typescript", "build-mode": "none"})
    print("codeql_matrix=" + json.dumps(matrix, separators=(",", ":")))


if __name__ == "__main__":
    main()
