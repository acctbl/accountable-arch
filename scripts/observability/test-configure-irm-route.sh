#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$root/scripts/observability/lib/evidence.sh"
work="$(mktemp -d "${TMPDIR:-/tmp}/accountable-irm-route-test.XXXXXX")"
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin" "$work/state"
ln -s "$root/scripts/observability/testdata/aws-irm" "$work/bin/aws"

write_evidence() {
	local irm="$1" observed_at="$2"
	jq -n --arg observed_at "$observed_at" --argjson irm "$irm" \
		'{state:"pass",observed_at:$observed_at,irm_included_at_no_cost:$irm}' >"$work/capacity.json"
}

run_route() {
	local scenario="$1"
	PATH="$work/bin:$PATH" \
	IRM_TEST_STATE_DIR="$work/state" \
	IRM_TEST_SCENARIO="$scenario" \
	AWS_REGION=eu-west-2 \
	ACCOUNTABLE_ENVIRONMENT=production \
	ACCOUNTABLE_CAPACITY_EVIDENCE="$work/capacity.json" \
	ACCOUNTABLE_CRITICAL_TOPIC_ARN=arn:aws:sns:eu-west-2:1:topic \
		bash "$root/scripts/observability/configure-irm-route.sh" >/dev/null
}

fresh="$(accountable_format_epoch "$(date -u +%s)")"
write_evidence false "$fresh"
run_route disable
[[ -e "$work/state/unsubscribed" && -e "$work/state/deleted" ]]

rm -f "$work/state"/*
write_evidence true "$fresh"
run_route existing
[[ -e "$work/state/stored" && ! -e "$work/state/subscribed" ]]

rm -f "$work/state"/*
run_route create
[[ -e "$work/state/subscribed" && -e "$work/state/stored" ]]

rm -f "$work/state"/*
set +e
run_route conflict
status=$?
set -e
[[ "$status" == "1" && ! -e "$work/state/stored" ]]

write_evidence true "$(accountable_format_epoch "$(($(date -u +%s) - 3600))")"
set +e
run_route existing
status=$?
set -e
[[ "$status" == "2" ]]

echo "irm-route-tests: pass"
