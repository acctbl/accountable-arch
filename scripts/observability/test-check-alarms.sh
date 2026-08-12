#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
tools="$(mktemp -d "${TMPDIR:-/tmp}/accountable-alarm-tools.XXXXXX")"
trap 'rm -rf "$tools"' EXIT
ln -s "$root/scripts/observability/testdata/aws-alarms" "$tools/aws"

run_case() {
	local test_case="$1" expected="$2" output
	output="$(
		PATH="$tools:$PATH" \
		AWS_REGION=eu-west-2 \
		ACCOUNTABLE_ENVIRONMENT=staging \
		ACCOUNTABLE_TEST_CASE="$test_case" \
		bash "$root/scripts/observability/check-alarms.sh"
	)"
	jq -e --arg expected "$expected" '.state == $expected' <<<"$output" >/dev/null
}

run_case pass pass
run_case warning pass
run_case critical fail
run_case malformed unknown
run_case invoke-error unknown
run_case function-error unknown

echo "test-check-alarms: pass"
