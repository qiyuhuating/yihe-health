"""Playwright browser launcher shared by end-to-end checks.

CI normally uses Playwright-managed Chromium. Local and restricted build
containers can point at an existing browser with YIHE_CHROMIUM_EXECUTABLE.
"""
from __future__ import annotations

import os
from pathlib import Path


def launch_chromium(playwright, **kwargs):
    executable = os.environ.get('YIHE_CHROMIUM_EXECUTABLE', '').strip()
    if executable:
        path = Path(executable)
        if not path.is_file():
            raise RuntimeError(f'YIHE_CHROMIUM_EXECUTABLE does not exist: {path}')
        kwargs['executable_path'] = str(path)
    return playwright.chromium.launch(**kwargs)
