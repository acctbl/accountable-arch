#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fake_bin="$(mktemp -d "${TMPDIR:-/tmp}/accountable-capacity-test.XXXXXX")"
trap 'rm -rf "$fake_bin"' EXIT
ln -s "$root/scripts/observability/testdata/aws" "$fake_bin/aws"

format_epoch() {
	local epoch="$1"
	date -u -d "@$epoch" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null ||
		date -u -r "$epoch" +%Y-%m-%dT%H:%M:%SZ
}

now="$(date -u +%s)"
fresh="$(format_epoch "$((now - 60))")"
stale="$(format_epoch "$((now - 3600))")"
conflicting="$(format_epoch "$((now - 960))")"

prove() {
	local test_case="$1" observed="$2" expected_status="$3" expected_state="$4"
	local output status
	set +e
	output="$(
		PATH="$fake_bin:$PATH" \
		AWS_REGION=eu-west-2 \
		CAPACITY_TEST_CASE="$test_case" \
		CAPACITY_OBSERVED_AT="$observed" \
		CAPACITY_CONFLICTING_AT="$conflicting" \
		bash "$root/scripts/observability/check-capacity.sh"
	)"
	status=$?
	set -e
	if [[ "$status" != "$expected_status" ]]; then
		echo "$test_case returned $status, expected $expected_status" >&2
		exit 1
	fi
	if ! jq -e --arg state "$expected_state" '.state == $state' <<<"$output" >/dev/null; then
		echo "$test_case did not return $expected_state evidence" >&2
		exit 1
	fi
}

prove pass "$fresh" 0 pass
prove fail "$fresh" 1 fail
prove stale "$stale" 2 unknown
prove missing "$fresh" 2 unknown
prove conflicting "$fresh" 2 unknown
prove malformed "$fresh" 2 unknown
prove incomplete "$fresh" 2 unknown

echo "capacity-gate-tests: pass"
