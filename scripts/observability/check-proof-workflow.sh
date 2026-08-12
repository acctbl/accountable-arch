#!/usr/bin/env bash
set -euo pipefail

python3 - <<'PY'
from pathlib import Path
import sys

import yaml

path = Path(".github/workflows/observability-proof.yml")
if not path.is_file():
    print(f"{path}: missing protected managed-proof workflow", file=sys.stderr)
    sys.exit(1)

document = yaml.safe_load(path.read_text())
triggers = document.get("on") or document.get(True) or {}
dispatch = triggers.get("workflow_dispatch") or {}
inputs = dispatch.get("inputs") or {}
expected_inputs = {"environment", "cell", "profile", "duration", "rate"}
if set(inputs) != expected_inputs:
    print(f"{path}: dispatch inputs must be exactly {sorted(expected_inputs)}", file=sys.stderr)
    sys.exit(1)

environment_options = set(inputs["environment"].get("options") or [])
profile_options = set(inputs["profile"].get("options") or [])
if environment_options != {"development", "staging", "production"}:
    print(f"{path}: every deployment environment must be supported", file=sys.stderr)
    sys.exit(1)
if profile_options != {"smoke", "baseline", "capacity", "soak", "controlled-failure"}:
    print(f"{path}: the complete load profile contract is required", file=sys.stderr)
    sys.exit(1)

workflow_text = path.read_text()
required_contract = [
    "environment:\n      name: ${{ inputs.environment }}",
    "ACCOUNTABLE_PROOF_KEY: ${{ secrets.ACCOUNTABLE_PROOF_KEY }}",
    "ManagedProofSuccess",
    "PrivacyProofSuccess",
    "PagingProofSuccess",
    "scripts/observability/prove-paging.sh",
    "scripts/observability/prove-task-uncertainty.sh",
    "if: always()",
    '"$ENVIRONMENT" == "production"',
    '"$PROFILE" != "smoke"',
]
for fragment in required_contract:
    if fragment not in workflow_text:
        print(f"{path}: missing contract fragment {fragment!r}", file=sys.stderr)
        sys.exit(1)

schedule_path = Path(".github/workflows/observability-proof-schedule.yml")
if not schedule_path.is_file():
    print(f"{schedule_path}: missing scheduled production proof", file=sys.stderr)
    sys.exit(1)
schedule_text = schedule_path.read_text()
for fragment in [
    "17 6 * * 1",
    "43 5 1 1,4,7,10 *",
    "./.github/workflows/observability-proof.yml",
    "environment: staging",
    "environment: production",
    "profile: controlled-failure",
]:
    if fragment not in schedule_text:
        print(f"{schedule_path}: missing schedule contract fragment {fragment!r}", file=sys.stderr)
        sys.exit(1)

print("check-proof-workflow: protected proof interface is complete")
PY
