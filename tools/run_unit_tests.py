"""Run stdlib tests with strict warnings and deterministic SQLite leak checks.

Python 3.13+ reports unclosed connections as ResourceWarning. Track ownership
on Python 3.12 too, and fail on warnings raised during object finalization.
"""
import argparse
import gc
import json
import sqlite3
import sys
import unittest
import warnings
from pathlib import Path
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--report', type=Path)
    args = parser.parse_args()
    sys.path.insert(0, str(ROOT / 'backend'))
    connections, unraisable = [], []
    original_connect, original_hook = sqlite3.connect, sys.unraisablehook

    def tracked_connect(*args, **kwargs):
        connection = original_connect(*args, **kwargs)
        connections.append(connection)
        return connection

    def check_unraisable(value):
        unraisable.append(value.exc_type.__name__)
        original_hook(value)

    warnings.simplefilter('error', ResourceWarning)
    sys.unraisablehook = check_unraisable
    try:
        with mock.patch.object(sqlite3, 'connect', tracked_connect):
            suite = unittest.defaultTestLoader.discover(str(ROOT / 'backend/tests'))
            result = unittest.TextTestRunner(verbosity=2).run(suite)
            gc.collect()
        leaks = 0
        for connection in connections:
            try:
                connection.execute('SELECT 1')
            except sqlite3.ProgrammingError:
                continue
            leaks += 1
            connection.close()
        gc.collect()
    finally:
        sys.unraisablehook = original_hook
    report = {
        'tests': result.testsRun, 'failures': len(result.failures),
        'errors': len(result.errors), 'skipped': len(result.skipped),
        'sqliteConnectionsOpened': len(connections), 'sqliteConnectionsLeaked': leaks,
        'unraisableErrors': unraisable, 'resourceWarningsAreErrors': True,
        'python': sys.version.split()[0],
    }
    print(json.dumps(report, indent=2))
    if args.report:
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(json.dumps(report, indent=2) + '\n', encoding='utf-8')
    return 0 if result.wasSuccessful() and not leaks and not unraisable else 1


if __name__ == '__main__':
    raise SystemExit(main())
