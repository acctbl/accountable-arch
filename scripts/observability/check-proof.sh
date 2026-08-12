#!/usr/bin/env bash
set -euo pipefail

environment="${ACCOUNTABLE_ENVIRONMENT:?set ACCOUNTABLE_ENVIRONMENT}"
cell="${ACCOUNTABLE_CELL:?set ACCOUNTABLE_CELL}"
release_id="${ACCOUNTABLE_RELEASE_ID:?set ACCOUNTABLE_RELEASE_ID}"
gate="${ACCOUNTABLE_GATE:?set ACCOUNTABLE_GATE}"
region="${AWS_REGION:?set AWS_REGION}"
now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
start="$(date -u -d '25 minutes ago' +%Y-%m-%dT%H:%M:%SZ)"
result="$(mktemp "${TMPDIR:-/tmp}/accountable-proof-gate.XXXXXX")"
trap 'rm -f "$result"' EXIT

case "$gate" in
	proof) metric=ManagedProofSuccess ;;
	privacy) metric=PrivacyProofSuccess ;;
	paging) metric=PagingProofSuccess ;;
	*) echo "the proof gate is invalid" >&2; exit 2 ;;
esac

if ! aws cloudwatch get-metric-data \
	--region "$region" \
	--start-time "$start" \
	--end-time "$now" \
	--scan-by TimestampDescending \
	--metric-data-queries "[{\"Id\":\"proof\",\"ReturnData\":true,\"MetricStat\":{\"Metric\":{\"Namespace\":\"Accountable/Observability\",\"MetricName\":\"$metric\",\"Dimensions\":[{\"Name\":\"Environment\",\"Value\":\"$environment\"},{\"Name\":\"CellId\",\"Value\":\"$cell\"},{\"Name\":\"ReleaseId\",\"Value\":\"$release_id\"}]},\"Period\":60,\"Stat\":\"Maximum\"}}]" \
	--output json >"$result"; then
	jq -n --arg observed_at "$now" '{state:"unknown",observed_at:$observed_at}'
	exit 0
fi

if ! jq -e '
  (.NextToken // "") == "" and
  (.MetricDataResults | length) == 1 and
  .MetricDataResults[0].Id == "proof" and
  (.MetricDataResults[0].Timestamps | length) >= 1 and
  (.MetricDataResults[0].Values | length) >= 1 and
  (.MetricDataResults[0].Timestamps | length) == (.MetricDataResults[0].Values | length)
' "$result" >/dev/null; then
	jq -n --arg observed_at "$now" '{state:"unknown",observed_at:$observed_at}'
	exit 0
fi

observed_at="$(date -u -d "$(jq -er '.MetricDataResults[0].Timestamps[0]' "$result")" +%Y-%m-%dT%H:%M:%SZ)"
value="$(jq -er '.MetricDataResults[0].Values[0]' "$result")"
if [[ "$value" == "1" ]]; then
	jq -n --arg observed_at "$observed_at" '{state:"pass",observed_at:$observed_at}'
else
	jq -n --arg observed_at "$observed_at" '{state:"fail",observed_at:$observed_at}'
fi
