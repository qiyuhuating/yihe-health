"""Decide whether the watch simulator matrix must run for a pull request."""

import os
import subprocess


ROOT_DOCS = {b"README.md", b"README.en.md"}


def documentation_only(diff: bytes) -> bool:
    fields = diff.split(b"\0")
    if len(fields) < 3 or len(fields) % 2 != 1 or fields[-1]:
        return False
    for header, path in zip(fields[:-1:2], fields[1::2], strict=True):
        parts = header.split()
        if len(parts) != 5 or (parts[0], parts[1], parts[4]) not in {
            (b":000000", b"100644", b"A"),
            (b":100644", b"100644", b"M"),
        }:
            return False
        if path not in ROOT_DOCS and not (path.startswith(b"docs/") and path.endswith(b".md")):
            return False
    return True


def main() -> None:
    comparison = f'{os.environ["BASE_SHA"]}...{os.environ["HEAD_SHA"]}'
    diff = subprocess.check_output(
        ["git", "diff", "--raw", "-z", "--no-renames", comparison, "--"]
    )
    run = "false" if documentation_only(diff) else "true"
    with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
        output.write(f"run={run}\n")
    print(f"Run watch simulator matrix: {run}")


if __name__ == "__main__":
    main()
