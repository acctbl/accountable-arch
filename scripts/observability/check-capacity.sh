#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/evidence.sh"

region="${AWS_REGION:?set AWS_REGION}"
stack="${GRAFANA_STACK_NAME:-acctbl}"

now_epoch="$(date -u +%s)"
now="$(accountable_format_epoch "$now_epoch")"
start="$(accountable_format_epoch "$((now_epoch - 2100))")"
queries="$(mktemp "${TMPDIR:-/tmp}/accountable-capacity.XXXXXX")"
result="$(mktemp "${TMPDIR:-/tmp}/accountable-capacity-result.XXXXXX")"
trap 'rm -f "$queries" "$result"' EXIT

jq -n --arg stack "$stack" '
  def metric($id; $name; $limit): {
    Id: $id,
    ReturnData: true,
    MetricStat: {
      Metric: {
        Namespace: "Accountable/GrafanaCapacity",
        MetricName: $name,
        Dimensions: ([{Name: "StackName", Value: $stack}] + (if $limit == "" then [] else [{Name: "Limit", Value: $limit}] end))
      },
      Period: 900,
      Stat: "Maximum"
    }
  };
  [
    metric("read"; "CapacityReadSuccess"; ""),
    metric("tier"; "TierPolicyCompliant"; ""),
    metric("fresh"; "CapacitySampleFresh"; ""),
    metric("irm"; "IRMIncludedAtNoCost"; ""),
    metric("shared"; "SharedAllocationPercent"; ""),
    {
      Id: "dev",
      ReturnData: true,
      MetricStat: {
        Metric: {
          Namespace: "Accountable/GrafanaCapacity",
          MetricName: "AllocatedCapacityPercent",
          Dimensions: [{Name: "StackName", Value: $stack}, {Name: "Environment", Value: "development"}]
        },
        Period: 900,
        Stat: "Maximum"
      }
    },
    {
      Id: "stage",
      ReturnData: true,
      MetricStat: {
        Metric: {
          Namespace: "Accountable/GrafanaCapacity",
          MetricName: "AllocatedCapacityPercent",
          Dimensions: [{Name: "StackName", Value: $stack}, {Name: "Environment", Value: "staging"}]
        },
        Period: 900,
        Stat: "Maximum"
      }
    },
    {
      Id: "prod",
      ReturnData: true,
      MetricStat: {
        Metric: {
          Namespace: "Accountable/GrafanaCapacity",
          MetricName: "AllocatedCapacityPercent",
          Dimensions: [{Name: "StackName", Value: $stack}, {Name: "Environment", Value: "production"}]
        },
        Period: 900,
        Stat: "Maximum"
      }
    },
    metric("a0"; "ForecastUsagePercent"; "alert_instances"),
    metric("a1"; "ForecastUsagePercent"; "alert_rules"),
    metric("g0"; "ForecastUsagePercent"; "grafana_users"),
    metric("i0"; "ForecastUsagePercent"; "irm_users"),
    metric("l0"; "ForecastUsagePercent"; "log_ingestion"),
    metric("m0"; "ForecastUsagePercent"; "metric_series"),
    metric("t0"; "ForecastUsagePercent"; "trace_ingestion"),
    metric("a2"; "CurrentUsagePercent"; "alert_instances"),
    metric("a3"; "CurrentUsagePercent"; "alert_rules"),
    metric("g1"; "CurrentUsagePercent"; "grafana_users"),
    metric("i1"; "CurrentUsagePercent"; "irm_users"),
    metric("l1"; "CurrentUsagePercent"; "log_ingestion"),
    metric("m1"; "CurrentUsagePercent"; "metric_series"),
    metric("t1"; "CurrentUsagePercent"; "trace_ingestion")
  ]' >"$queries"

aws cloudwatch get-metric-data \
  --region "$region" \
  --start-time "$start" \
  --end-time "$now" \
  --scan-by TimestampDescending \
  --metric-data-queries "file://$queries" \
  --output json >"$result"

jq -e '
  .MetricDataResults as $series |
  ["read", "tier", "fresh", "irm", "shared", "dev", "stage", "prod", "a0", "a1", "g0", "i0", "l0", "m0", "t0", "a2", "a3", "g1", "i1", "l1", "m1", "t1"] as $required |
  ($series | map(select((.Id as $id | $required | index($id)) != null))) as $tracked |
  ($tracked | length) == ($required | length) and
  (($tracked | map(.Id) | unique | length) == ($required | length)) and
  ((.NextToken // "") == "") and
  ($tracked | all(.StatusCode == "Complete" and ((.Messages // []) | length) == 0)) and
  ($tracked | all((.Timestamps | length) >= 1 and (.Values | length) >= 1)) and
  ($tracked | all((.Timestamps | length) == (.Values | length))) and
  ($tracked | all((.Timestamps[0] | fromdateiso8601?) != null and (.Values[0] | type) == "number")) and
  (($tracked | map(.Timestamps[0]) | unique | length) == 1)
' "$result" >/dev/null || {
  jq -n '{state:"unknown",reason:"capacity sample is missing, stale, conflicting, or incomplete"}'
  exit 2
}

observed_at="$(jq -r '.MetricDataResults[] | select(.Id == "read") | .Timestamps[0]' "$result")"
observed_epoch="$(accountable_parse_instant "$observed_at")"
observed_at="$(accountable_format_epoch "$observed_epoch")"
if (( now_epoch - observed_epoch > 1200 || observed_epoch > now_epoch )); then
  jq -n --arg observed_at "$observed_at" '{state:"unknown",observed_at:$observed_at,reason:"capacity sample is stale"}'
  exit 2
fi

if ! jq -e '
  (.MetricDataResults | map({key:.Id,value:.Values[0]}) | from_entries) as $values |
  $values.read == 1 and $values.tier == 1 and $values.fresh == 1 and
  ($values.irm == 0 or $values.irm == 1) and
  $values.shared == 50 and $values.dev == 10 and $values.stage == 15 and $values.prod == 25 and
  ([$values.a0, $values.a1, $values.g0, $values.i0, $values.l0, $values.m0, $values.t0,
    $values.a2, $values.a3, $values.g1, $values.i1, $values.l1, $values.m1, $values.t1] | all(. < 85 and . >= 0))
' "$result" >/dev/null; then
  jq -n --arg observed_at "$observed_at" '{state:"fail",observed_at:$observed_at,reason:"capacity policy or critical threshold failed"}'
  exit 1
fi

jq --arg observed_at "$observed_at" '
  (.MetricDataResults | map({key:.Id,value:.Values[0]}) | from_entries) as $values |
  {
    state:"pass",
    observed_at:$observed_at,
    capacity_read_success:($values.read == 1),
    tier_policy_compliant:($values.tier == 1),
    sample_fresh:($values.fresh == 1),
    irm_included_at_no_cost:($values.irm == 1),
    shared_allocation_percent:$values.shared,
    environment_allocations:{development:$values.dev,staging:$values.stage,production:$values.prod},
    current_usage_percent:{
      alert_instances:$values.a2,
      alert_rules:$values.a3,
      grafana_users:$values.g1,
      irm_users:$values.i1,
      log_ingestion:$values.l1,
      metric_series:$values.m1,
      trace_ingestion:$values.t1
    },
    forecast_usage_percent:{
      alert_instances:$values.a0,
      alert_rules:$values.a1,
      grafana_users:$values.g0,
      irm_users:$values.i0,
      log_ingestion:$values.l0,
      metric_series:$values.m0,
      trace_ingestion:$values.t0
    }
  }
' "$result"
