#!/usr/bin/env bash
set -euo pipefail

environment="${ACCOUNTABLE_ENVIRONMENT:?set ACCOUNTABLE_ENVIRONMENT}"
cells_csv="${ACCOUNTABLE_CELLS:?set ACCOUNTABLE_CELLS}"
release_bucket="${ACCOUNTABLE_RELEASE_BUCKET:?set ACCOUNTABLE_RELEASE_BUCKET}"
window_start="${ACCOUNTABLE_WINDOW_START:?set ACCOUNTABLE_WINDOW_START}"
window_end="${ACCOUNTABLE_WINDOW_END:?set ACCOUNTABLE_WINDOW_END}"
region="${AWS_REGION:?set AWS_REGION}"
evaluator="${ACCOUNTABLE_BUDGET_EVALUATOR:-budget-snapshot}"
now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
query_file="$(mktemp "${TMPDIR:-/tmp}/accountable-budget-queries.XXXXXX")"
metric_file="$(mktemp "${TMPDIR:-/tmp}/accountable-budget-metrics.XXXXXX")"
snapshot_file="$(mktemp "${TMPDIR:-/tmp}/accountable-budget-snapshot.XXXXXX")"
inventory_file="$(mktemp "${TMPDIR:-/tmp}/accountable-budget-inventory.XXXXXX")"
trap 'rm -f "$query_file" "$metric_file" "$snapshot_file" "$inventory_file"' EXIT

case "$evaluator" in
	budget-snapshot|staging-observation-snapshot) ;;
	*)
		echo "invalid budget evaluator" >&2
		exit 2
		;;
esac

IFS=',' read -r -a cells <<<"$cells_csv"
if (( ${#cells[@]} == 0 )); then
	echo "at least one registered cell is required" >&2
	exit 2
fi

jq -n '{}' >"$inventory_file"
for cell in "${cells[@]}"; do
	if [[ ! "$cell" =~ ^[-A-Za-z0-9._]{1,64}$ ]]; then
		echo "invalid cell identifier" >&2
		exit 2
	fi
	chain="$(go run ./cmd/cell-inventory read \
		--bucket "$release_bucket" \
		--region "$region" \
		--environment "$environment" \
		--cell-id "$cell")"
	if ! jq -e 'length > 0' <<<"$chain" >/dev/null; then
		echo "the inventory chain for $cell is missing" >&2
		exit 2
	fi
	jq --arg cell "$cell" --argjson chain "$chain" '. + {($cell): $chain}' "$inventory_file" >"$inventory_file.next"
	mv "$inventory_file.next" "$inventory_file"
done

jq -n --arg environment "$environment" --argjson cells "$(printf '%s\n' "${cells[@]}" | jq -R . | jq -s .)" '
  def metric($id; $name; $dimensions): {
    Id: $id,
    ReturnData: true,
    MetricStat: {
      Metric: {Namespace: "Accountable/SLO", MetricName: $name, Dimensions: $dimensions},
      Period: 300,
      Stat: "Sum"
    }
  };
  ([
    metric("eligible"; "EligibleRequestCount"; [{Name:"Environment",Value:$environment},{Name:"ServiceName",Value:"accountable-api"}]),
    metric("bad"; "AvailabilityBadRequestCount"; [{Name:"Environment",Value:$environment},{Name:"ServiceName",Value:"accountable-api"}]),
    metric("slow"; "LatencySlowRequestCount"; [{Name:"Environment",Value:$environment},{Name:"ServiceName",Value:"accountable-api"}]),
    {
      Id: "uncertain",
      ReturnData: true,
      Expression: ("SUM(SEARCH(\u0027{Accountable/Collector,Environment} MetricName=\"SLIDeliveryUncertain\" AND Environment=\"" + $environment + "\"\u0027,\u0027Sum\u0027,300))")
    }
  ] + ($cells | to_entries | map(
    metric(("heartbeat" + (.key|tostring)); "SLIExportHeartbeatCount"; [
      {Name:"Environment",Value:$environment},
      {Name:"ServiceName",Value:"accountable-api"},
      {Name:"CellId",Value:.value}
    ])
  )))
' >"$query_file"

aws cloudwatch get-metric-data \
	--region "$region" \
	--start-time "$window_start" \
	--end-time "$window_end" \
	--scan-by TimestampAscending \
	--max-datapoints 100800 \
	--metric-data-queries "file://$query_file" \
	--output json >"$metric_file"

if [[ "$(jq -r '.NextToken // ""' "$metric_file")" != "" ]]; then
	echo "the budget metric query was paginated" >&2
	exit 2
fi

jq -e --argjson count "$((4 + ${#cells[@]}))" '
  ((.NextToken // "") == "") and
  (.MetricDataResults | length) == $count and
  (.MetricDataResults | map(.Id) | unique | length) == $count and
  (.MetricDataResults | all(.StatusCode == "Complete" and ((.Messages // []) | length) == 0)) and
  (.MetricDataResults | all((.Timestamps | length) == (.Values | length))) and
  (.MetricDataResults | map(.Timestamps[]) | all((fromdateiso8601? != null))) and
  (.MetricDataResults | map(.Values[]) | all(type == "number" and . >= 0 and floor == .))
' "$metric_file" >/dev/null

jq -n \
	--arg now "$now" \
	--arg start "$window_start" \
	--arg end "$window_end" \
	--argjson cells "$(printf '%s\n' "${cells[@]}" | jq -R . | jq -s .)" \
	--argjson inventory "$(jq -c . "$inventory_file")" \
	--argjson metrics "$(jq -c . "$metric_file")" '
  def points($id):
    ($metrics.MetricDataResults[] | select(.Id == $id)) as $series |
    [range(0; ($series.Values | length)) | {started_at:$series.Timestamps[.], value:$series.Values[.]}];
  {
    now:$now,
    window_start:$start,
    window_end:$end,
    expected_cells:$cells,
    inventory:$inventory,
    eligible:points("eligible"),
    availability_bad:points("bad"),
    latency_slow:points("slow"),
    heartbeats:($cells | to_entries | map({key:.value,value:points("heartbeat" + (.key|tostring))}) | from_entries),
    delivery_uncertain:([points("uncertain")[].value] | any(. > 0))
  }
' >"$snapshot_file"

go run ./cmd/observability-gate "$evaluator" <"$snapshot_file"
