#!/usr/bin/env bash
set -euo pipefail

# Two deployment blockers reached main as a broken YAML block and a shell command
# that no gate parsed. This check parses every workflow and every run block.

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

python3 - "$@" <<'PY'
import pathlib
import subprocess
import sys
import tempfile

try:
    import yaml
except ModuleNotFoundError:
    print("check-workflows: PyYAML is unavailable; install it to run this gate", file=sys.stderr)
    sys.exit(1)

failures = 0
for path in sorted(pathlib.Path(".github/workflows").glob("*.yml")):
    try:
        document = yaml.safe_load(path.read_text())
    except yaml.YAMLError as error:
        print(f"{path}: invalid YAML: {error}", file=sys.stderr)
        failures += 1
        continue

    for job_name, job in (document.get("jobs") or {}).items():
        for index, step in enumerate(job.get("steps") or []):
            script = step.get("run")
            if not script:
                continue
            with tempfile.NamedTemporaryFile("w", suffix=".sh", delete=False) as handle:
                handle.write("set -euo pipefail\n")
                handle.write(script)
                script_path = handle.name
            result = subprocess.run(
                ["bash", "-n", script_path], capture_output=True, text=True, check=False
            )
            pathlib.Path(script_path).unlink()
            if result.returncode != 0:
                label = step.get("name", f"step {index}")
                print(f"{path}: job {job_name}: {label}: {result.stderr.strip()}", file=sys.stderr)
                failures += 1

if failures:
    sys.exit(1)
print("check-workflows: every workflow and run block parses")
PY
