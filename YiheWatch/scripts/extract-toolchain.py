#!/usr/bin/env python3
"""Verify the pinned official archive before extracting, including Windows long paths."""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import zipfile


def extract(archive: Path, destination: Path):
    expected = json.loads((Path(__file__).resolve().parents[1] / "toolchain.json").read_text(encoding="utf-8"))
    digest = hashlib.sha256()
    with archive.open("rb") as source:
        for block in iter(lambda: source.read(8 * 1024 * 1024), b""):
            digest.update(block)
    if digest.hexdigest() != expected["archiveSha256"]:
        raise ValueError("SDK archive differs from the pinned SHA-256; nothing extracted")
    destination = destination.resolve()
    if destination.exists():
        raise FileExistsError("SDK destination already exists; refusing to overwrite it")
    with zipfile.ZipFile(archive) as source:
        entries = source.infolist()
        for entry in entries:
            path = PurePosixPath(entry.filename)
            if path.is_absolute() or ".." in path.parts or "\\" in entry.filename or any(":" in p for p in path.parts):
                raise ValueError("Unsafe SDK archive member")
            if not path.parts or path.parts[0] != "command-line-tools" or (entry.external_attr >> 16) & 0o170000 == 0o120000:
                raise ValueError("Unexpected SDK archive member")
        destination.mkdir(parents=True)
        for entry in entries:
            target = str(destination.joinpath(*PurePosixPath(entry.filename).parts))
            # Python's normal Windows paths otherwise fail on SDK legacy compiler assets.
            if os.name == "nt":
                target = "\\\\?\\UNC\\" + target[2:] if target.startswith("\\\\") else "\\\\?\\" + target
            target = Path(target)
            if entry.is_dir():
                target.mkdir(parents=True, exist_ok=True)
            else:
                target.parent.mkdir(parents=True, exist_ok=True)
                with source.open(entry) as reader, target.open("wb") as writer:
                    shutil.copyfileobj(reader, writer, 1024 * 1024)
    report = {"sha256": digest.hexdigest(), "bytes": archive.stat().st_size, "members": len(entries), "crcVerified": True}
    (destination / "extraction.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(report))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("archive", type=Path)
    parser.add_argument("destination", type=Path)
    args = parser.parse_args()
    extract(args.archive, args.destination)
