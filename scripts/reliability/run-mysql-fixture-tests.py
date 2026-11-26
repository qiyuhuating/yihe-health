#!/usr/bin/env python3
"""Opt-in cross-package MySQL test fixtures; never cache test-mutated data."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time


def main():
    if not os.environ.get("TEST_MYSQL_DSN"):
        raise SystemExit("Set TEST_MYSQL_DSN to a disposable MySQL 8.4 instance")
    backend = Path(__file__).resolve().parents[2] / "backend"
    env = os.environ.copy()
    env.pop("TEST_MYSQL_SHARED_FIXTURE", None)
    env.pop("TEST_MYSQL_EXPORT_FIXTURE", None)
    with tempfile.TemporaryDirectory(prefix="yihe-fixture-") as directory:
        archive = Path(directory) / "pristine.json"
        env["TEST_MYSQL_EXPORT_FIXTURE"] = str(archive)
        start = time.monotonic()
        subprocess.run(
            ["go", "test", "-count=1", "-run", "^TestPrepareSharedMySQLFixture$", "./internal/db/dbtest"],
            cwd=backend, env=env, check=True,
        )
        if not archive.is_file():
            raise SystemExit("Fixture preparation did not produce an archive")
        print(f"Pristine fixture preparation: {time.monotonic() - start:.3f}s", flush=True)
        del env["TEST_MYSQL_EXPORT_FIXTURE"]
        env["TEST_MYSQL_SHARED_FIXTURE"] = str(archive)
        start = time.monotonic()
        args = sys.argv[1:] or ["./..."]
        result = subprocess.run(
            ["go", "test", "-race", "-count=1", "-timeout", "45m", *args],
            cwd=backend, env=env,
        )
        print(f"Shared fixture test run: {time.monotonic() - start:.3f}s", flush=True)
        return result.returncode


if __name__ == "__main__":
    raise SystemExit(main())
