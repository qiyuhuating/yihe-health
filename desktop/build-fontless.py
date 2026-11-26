#!/usr/bin/env python3
"""Build standalone Go servers with system-font UI for Windows and Linux."""
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
from tempfile import TemporaryDirectory

desktop = Path(__file__).resolve().parent
project = desktop.parent
backend = project / "backend"
frontend_dist = project / "frontend" / "dist"
web = backend / "internal" / "handlers" / "web"
version = json.loads((desktop / "version.json").read_text(encoding="utf-8"))
channel = os.environ.get("YIHE_RELEASE_CHANNEL", version["channel"])
if channel not in ("preview", "stable"):
    raise SystemExit("Invalid release channel")
release = project / "outputs" / f"v{version['version']}-{channel}" / "release"
release.mkdir(parents=True, exist_ok=True)

targets = (
    ("windows", "amd64", "win-x64.exe"),
    ("linux", "amd64", "linux-x64.tar.gz"),
    ("linux", "arm64", "linux-arm64.tar.gz"),
)
built = []

if not (frontend_dist / "index.html").is_file():
    raise SystemExit("Frontend production build is required before font-free native builds.")

with TemporaryDirectory(prefix="yihe-web-backup-") as temporary:
    saved = Path(temporary) / "web"
    shutil.move(str(web), str(saved))
    try:
        shutil.copytree(frontend_dist, web)
        fonts = web / "static" / "fonts"
        if not fonts.is_dir():
            raise RuntimeError("Expected bundled web fonts were not found.")
        shutil.rmtree(fonts)

        faces = 0
        for css in web.rglob("*.css"):
            source = css.read_text(encoding="utf-8")
            source, count = re.subn(r"@font-face\s*\{[^{}]*\}", "", source)
            faces += count
            source = re.sub(
                r"--care-font-family\s*:\s*[^;]+;",
                '--care-font-family: "Microsoft YaHei", "PingFang SC", "Noto Sans CJK SC", sans-serif;',
                source,
            )
            if "/static/fonts/" in source:
                raise RuntimeError(f"Bundled font reference remains in {css}")
            css.write_text(source, encoding="utf-8")
        if faces < 1:
            raise RuntimeError("No bundled font-face rules found to remove.")

        for goos, goarch, suffix in targets:
            output = release / f"Yihe-Native-NoFonts-{version['version']}-{suffix}"
            output.unlink(missing_ok=True)
            built.append(output)
            env = dict(os.environ, GOOS=goos, GOARCH=goarch, CGO_ENABLED="0")
            binary = output if goos == "windows" else Path(temporary) / f"yihe-server-{goarch}"
            subprocess.run(
                ["go", "build", "-trimpath", "-ldflags=-s -w", "-o", str(binary), "./cmd/server"],
                cwd=backend, env=env, check=True,
            )
            if goos == "linux":
                with tarfile.open(output, "w:gz") as archive:
                    member = archive.gettarinfo(str(binary), arcname="yihe-server")
                    member.mode = 0o755
                    with binary.open("rb") as stream:
                        archive.addfile(member, stream)
            if not output.is_file() or output.stat().st_size == 0:
                raise RuntimeError(f"Missing or empty native distribution: {output.name}")
            print(f"{output.name}: {output.stat().st_size} bytes")
        print(f"Font-free native builds passed; removed {faces} embedded @font-face rules.")
    except BaseException:
        for output in built:
            output.unlink(missing_ok=True)
        raise
    finally:
        shutil.rmtree(web)
        shutil.move(str(saved), str(web))
