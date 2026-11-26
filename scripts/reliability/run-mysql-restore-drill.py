#!/usr/bin/env python3
"""Run the isolated MySQL restore drill and require its actual passing event."""
import argparse
import json
import os
from pathlib import Path
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path, help="new path for raw Go test events")
    args = parser.parse_args()
    if not os.environ.get("TEST_MYSQL_DSN"):
        parser.error("set TEST_MYSQL_DSN to a dedicated MySQL test instance")
    backend = Path(__file__).resolve().parents[2] / "backend"
    test_name = "TestMySQLCoordinatedBackupRestore"
    with args.output.open("xb") as events:
        result = subprocess.run(
            ["go", "test", "-json", "-race", "-count=1", "-timeout=5m", "-run", f"^{test_name}$", "./internal/handlers"],
            cwd=backend, stdout=events, stderr=subprocess.STDOUT, check=False,
        )
    action = ""
    for line in args.output.read_text(encoding="utf-8").splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        if event.get("Test") == test_name and event.get("Action") in {"pass", "fail", "skip"}:
            action = event["Action"]
    if result.returncode != 0 or action != "pass":
        parser.exit(1, "restore drill did not pass; inspect the local event file\n")
    print("Isolated MySQL restore drill passed")


if __name__ == "__main__":
    main()
