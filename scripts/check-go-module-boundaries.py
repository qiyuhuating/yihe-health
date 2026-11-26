"""Enforce Go package dependency direction without changing existing transaction boundaries."""
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
BACKEND = ROOT / "backend"
PREFIX = "yihe-care/internal/"
# Existing atomic cross-domain writes, pending transaction-preserving migration.
EXISTING_CROSS_REPO = {
    ("authrepo", "carerepo"),          # first-run supervisor assignment
    ("devicerepo", "telemetryrepo"),    # device binding and initial watch thresholds
    ("telemetryrepo", "vitalrepo"),     # same-transaction vital encryption; one authenticated writer
}

result = subprocess.run(
    ["go", "list", "-f", '{{.ImportPath}}|{{join .Imports " "}}', "./internal/..."],
    cwd=BACKEND, capture_output=True, text=True, encoding="utf-8",
)
if result.returncode:
    print(result.stderr, file=sys.stderr)
    raise SystemExit(result.returncode)

errors = []
packages = set()
edges = 0
for line in result.stdout.splitlines():
    source, sep, imports = line.partition("|")
    if not sep or not source.startswith(PREFIX):
        continue
    source = source[len(PREFIX):]
    packages.add(source)
    for dependency in imports.split():
        if not dependency.startswith(PREFIX):
            continue
        dependency = dependency[len(PREFIX):]
        edges += 1
        if source.endswith("repo"):
            if dependency == "handlers" or dependency.endswith("workflow"):
                errors.append(f"{source} -> {dependency}: repository depends on application layer")
            if dependency.endswith("repo") and source != dependency and (
                source, dependency
            ) not in EXISTING_CROSS_REPO:
                errors.append(f"{source} -> {dependency}: new cross-domain repository coupling")
        if source.endswith("policy") and (
            dependency == "handlers" or dependency.endswith(("repo", "workflow"))
        ):
            errors.append(f"{source} -> {dependency}: domain policy depends on persistence/application")

if errors:
    print("\n".join(errors), file=sys.stderr)
    raise SystemExit(1)
print(f"Go module boundaries OK: {len(packages)} packages, {edges} internal imports; "
      f"{len(EXISTING_CROSS_REPO)} documented transaction-scoped exceptions")
