#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$root/scripts/observability/lib/evidence.sh"
work="$(mktemp -d "${TMPDIR:-/tmp}/accountable-paging-test.XXXXXX")"
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin" "$work/state"
ln -s "$root/scripts/observability/testdata/aws-paging" "$work/bin/aws"
ln -s "$root/scripts/observability/testdata/curl-paging" "$work/bin/curl"

write_evidence() {
	local irm="$1"
	jq -n --arg observed_at "$(accountable_format_epoch "$(date -u +%s)")" --argjson irm "$irm" \
		'{state:"pass",observed_at:$observed_at,irm_included_at_no_cost:$irm}' >"$work/capacity.json"
}

run_proof() {
	local scenario="$1"
	PATH="$work/bin:$PATH" \
	PAGING_TEST_STATE_DIR="$work/state" \
	PAGING_TEST_SCENARIO="$scenario" \
	AWS_REGION=eu-west-2 \
	ACCOUNTABLE_ENVIRONMENT=production \
	ACCOUNTABLE_CAPACITY_EVIDENCE="$work/capacity.json" \
	ACCOUNTABLE_GRAFANA_STACK_URL=https://acctbl.grafana.net \
	ACCOUNTABLE_IRM_API_URL=https://irm.example.invalid \
		bash "$root/scripts/observability/prove-paging.sh" >/dev/null
}

write_evidence false
run_proof email
[[ -e "$work/state/fired" && -e "$work/state/recovered" ]]

rm -f "$work/state"/*
write_evidence true
run_proof irm
[[ -e "$work/state/fired" && -e "$work/state/recovered" ]]

rm -f "$work/state"/*
write_evidence false
set +e
run_proof missing-email
status=$?
set -e
[[ "$status" == "1" && ! -e "$work/state/fired" ]]

echo "paging-proof-tests: pass"
