#!/usr/bin/env bash
set -euo pipefail

environment="${ACCOUNTABLE_ENVIRONMENT:?set ACCOUNTABLE_ENVIRONMENT}"
region="${AWS_REGION:?set AWS_REGION}"
observed_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
alarms="$(mktemp "${TMPDIR:-/tmp}/accountable-alarms.XXXXXX")"
canary_invoke="$(mktemp "${TMPDIR:-/tmp}/accountable-canary-invoke.XXXXXX")"
canary_response="$(mktemp "${TMPDIR:-/tmp}/accountable-canary-response.XXXXXX")"
trap 'rm -f "$alarms" "$canary_invoke" "$canary_response"' EXIT

if ! aws lambda invoke \
	--region "$region" \
	--function-name "accountable-$environment-observability-canary" \
	"$canary_response" \
	--output json >"$canary_invoke"; then
	jq -n --arg observed_at "$observed_at" '{state:"unknown",observed_at:$observed_at}'
	exit 0
fi

if ! jq -e '(.FunctionError // "") == "" and .StatusCode == 200' "$canary_invoke" >/dev/null; then
	jq -n --arg observed_at "$observed_at" '{state:"unknown",observed_at:$observed_at}'
	exit 0
fi

if ! aws cloudwatch describe-alarms \
	--region "$region" \
	--state-value ALARM \
	--alarm-name-prefix "accountable-$environment-" \
	--output json >"$alarms"; then
	jq -n --arg observed_at "$observed_at" '{state:"unknown",observed_at:$observed_at}'
	exit 0
fi

if ! jq -e '
  [.MetricAlarms[], .CompositeAlarms[]] |
  all(try (.AlarmDescription | fromjson | type == "object") catch false)
' "$alarms" >/dev/null; then
	jq -n --arg observed_at "$observed_at" '{state:"unknown",observed_at:$observed_at}'
	exit 0
fi

critical="$(jq '[((.MetricAlarms[], .CompositeAlarms[]) | select((.AlarmDescription | fromjson).severity == "critical"))] | length' "$alarms")"
if (( critical > 0 )); then
	jq -n --arg observed_at "$observed_at" --argjson count "$critical" '{state:"fail",observed_at:$observed_at,critical_alarm_count:$count}'
	exit 0
fi

jq -n --arg observed_at "$observed_at" '{state:"pass",observed_at:$observed_at,critical_alarm_count:0}'
