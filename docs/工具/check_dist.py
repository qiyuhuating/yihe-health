#!/usr/bin/env python3
"""Validate that the built frontend artifact is HTTP-only and excludes demo data."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DIST = ROOT / "dist"
FORBIDDEN = (
    "js/config.js",
    "js/demo-credentials.js",
    "js/admin-credentials.js",
    "js/seed-data.js",
    "data/residents.js",
)
CONFIG = DIST / "js" / "care-runtime-config.js"


def main():
    errors = []
    if not DIST.is_dir():
        errors.append("dist/ is missing; run the frontend build first")
    else:
        for relative in FORBIDDEN:
            if (DIST / relative).exists():
                errors.append(f"forbidden artifact: {relative}")

    if not CONFIG.is_file():
        errors.append("dist/js/care-runtime-config.js is missing")
    else:
        config = CONFIG.read_text(encoding="utf-8")
        http_modes = config.count("mode:'http'")
        demo_modes = config.count("'demo'")
        if http_modes != 1:
            errors.append(f"expected one mode:'http', found {http_modes}")
        if demo_modes:
            errors.append(f"expected no 'demo' mode, found {demo_modes}")

    if errors:
        for error in errors:
            print(f"[FAIL] {error}")
        return 1
    print("[OK] dist is HTTP-only and excludes demo credentials and resident seed data")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
