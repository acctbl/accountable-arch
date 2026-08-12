#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/evidence.sh"

environment="${ACCOUNTABLE_ENVIRONMENT:?set ACCOUNTABLE_ENVIRONMENT}"
cell="${ACCOUNTABLE_CELL:?set ACCOUNTABLE_CELL}"
cluster="${ACCOUNTABLE_CLUSTER:?set ACCOUNTABLE_CLUSTER}"
service="${ACCOUNTABLE_COLLECTOR_SERVICE:?set ACCOUNTABLE_COLLECTOR_SERVICE}"
window_start="${ACCOUNTABLE_WINDOW_START:?set ACCOUNTABLE_WINDOW_START}"
window_end="${ACCOUNTABLE_WINDOW_END:?set ACCOUNTABLE_WINDOW_END}"
region="${AWS_REGION:?set AWS_REGION}"
now_epoch="$(date -u +%s)"
now="$(accountable_format_epoch "$now_epoch")"
recent_start="$(accountable_format_epoch "$((now_epoch - 300))")"
tasks_file="$(mktemp "${TMPDIR:-/tmp}/accountable-loss-tasks.XXXXXX")"
queries_file="$(mktemp "${TMPDIR:-/tmp}/accountable-loss-queries.XXXXXX")"
terminal_file="$(mktemp "${TMPDIR:-/tmp}/accountable-loss-terminal.XXXXXX")"
pending_file="$(mktemp "${TMPDIR:-/tmp}/accountable-loss-pending.XXXXXX")"
input_file="$(mktemp "${TMPDIR:-/tmp}/accountable-loss-input.XXXXXX")"
trap 'rm -f "$tasks_file" "$queries_file" "$terminal_file" "$pending_file" "$input_file"' EXIT

desired="$(aws ecs describe-services --region "$region" --cluster "$cluster" --services "$service" --query 'services[0].desiredCount' --output text)"
aws ecs list-tasks \
	--region "$region" \
	--cluster "$cluster" \
	--service-name "$service" \
	--desired-status RUNNING \
	--query 'taskArns' \
	--output json | jq '[.[] | split("/")[-1]] | sort' >"$tasks_file"

if [[ ! "$desired" =~ ^[1-9][0-9]*$ ]] || [[ "$(jq length "$tasks_file")" -ne "$desired" ]]; then
	echo '{"state":"unknown"}'
	exit 0
fi

jq -n --arg environment "$environment" --arg cell "$cell" '
  def metric($id; $name; $pipeline; $signal; $period; $stat; $task): {
    Id:$id,
    ReturnData:true,
    MetricStat:{
      Metric:{
        Namespace:"Accountable/Collector",
        MetricName:$name,
        Dimensions:([ {Name:"Environment",Value:$environment}, {Name:"CellId",Value:$cell} ] +
          (if $pipeline == "" then [] else [{Name:"Pipeline",Value:$pipeline},{Name:"Signal",Value:$signal}] end) +
          (if $task == "" then [] else [{Name:"CollectorTaskId",Value:$task}] end))
      },
      Period:$period,
      Stat:$stat
    }
  };
  [
    metric("sent_traces";"SentItemCount";"traces/app";"traces";3600;"Sum";""),
    metric("drop_traces";"PermanentlyDroppedItemCount";"traces/app";"traces";3600;"Sum";""),
    metric("refused_traces";"RefusedItemCount";"traces/app";"traces";3600;"Sum";""),
    metric("sent_metrics";"SentItemCount";"metrics/app";"metrics";3600;"Sum";""),
    metric("drop_metrics";"PermanentlyDroppedItemCount";"metrics/app";"metrics";3600;"Sum";""),
    metric("refused_metrics";"RefusedItemCount";"metrics/app";"metrics";3600;"Sum";""),
    metric("sent_logs";"SentItemCount";"logs/app";"logs";3600;"Sum";""),
    metric("drop_logs";"PermanentlyDroppedItemCount";"logs/app";"logs";3600;"Sum";""),
    metric("refused_logs";"RefusedItemCount";"logs/app";"logs";3600;"Sum";""),
    metric("delivery_uncertain";"CollectorDeliveryUncertain";"";"";3600;"Sum";""),
    metric("accounting_uncertain";"CollectorAccountingUncertain";"";"";3600;"Sum";"")
  ]
' >"$queries_file"

aws cloudwatch get-metric-data \
	--region "$region" \
	--start-time "$window_start" \
	--end-time "$window_end" \
	--scan-by TimestampAscending \
	--max-datapoints 100800 \
	--metric-data-queries "file://$queries_file" \
	--output json >"$terminal_file"

if ! jq -e '
  ["sent_traces","drop_traces","refused_traces","sent_metrics","drop_metrics","refused_metrics","sent_logs","drop_logs","refused_logs","delivery_uncertain","accounting_uncertain"] as $required |
  ((.NextToken // "") == "") and
  (.MetricDataResults | length) == ($required | length) and
  (.MetricDataResults | map(.Id) | unique | length) == ($required | length) and
  (.MetricDataResults | all((.Id as $id | $required | index($id)) != null)) and
  (.MetricDataResults | all(.StatusCode == "Complete" and ((.Messages // []) | length) == 0)) and
  (.MetricDataResults | all((.Timestamps | length) == (.Values | length))) and
  (.MetricDataResults | map(.Values[]) | all(type == "number" and . >= 0 and floor == .))
' "$terminal_file" >/dev/null; then
	echo '{"state":"unknown"}'
	exit 0
fi

jq -n --arg environment "$environment" --arg cell "$cell" --slurpfile tasks "$tasks_file" '
  def metric($id; $name; $pipeline; $signal; $task): {
    Id:$id,
    ReturnData:true,
    MetricStat:{
      Metric:{
        Namespace:"Accountable/Collector",
        MetricName:$name,
        Dimensions:[
          {Name:"Environment",Value:$environment},
          {Name:"CellId",Value:$cell},
          {Name:"Pipeline",Value:$pipeline},
          {Name:"Signal",Value:$signal},
          {Name:"CollectorTaskId",Value:$task}
        ]
      },
      Period:60,
      Stat:"Maximum"
    }
  };
  [
    {pipeline:"traces/app",signal:"traces",key:"traces"},
    {pipeline:"metrics/app",signal:"metrics",key:"metrics"},
    {pipeline:"logs/app",signal:"logs",key:"logs"}
  ] as $signals |
  [$tasks[0][] as $task | $signals[] as $signal |
    metric(("pending_" + $signal.key + "_" + ($tasks[0] | index($task) | tostring));"PendingItemCount";$signal.pipeline;$signal.signal;$task),
    metric(("age_" + $signal.key + "_" + ($tasks[0] | index($task) | tostring));"OldestPendingItemAgeSeconds";$signal.pipeline;$signal.signal;$task)
  ]
' >"$queries_file"

aws cloudwatch get-metric-data \
	--region "$region" \
	--start-time "$recent_start" \
	--end-time "$now" \
	--scan-by TimestampDescending \
	--max-datapoints 100800 \
	--metric-data-queries "file://$queries_file" \
	--output json >"$pending_file"

expected_pending_series="$((desired * 6))"
jq -e --argjson expected "$expected_pending_series" '
  ((.NextToken // "") == "") and
  (.MetricDataResults | length) == $expected and
  (.MetricDataResults | map(.Id) | unique | length) == $expected and
  (.MetricDataResults | all(.StatusCode == "Complete" and ((.Messages // []) | length) == 0)) and
  (.MetricDataResults | all((.Values | length) >= 1 and (.Timestamps | length) >= 1)) and
  (.MetricDataResults | all((.Values | length) == (.Timestamps | length))) and
  (.MetricDataResults | map(.Values[]) | all(type == "number" and . >= 0 and floor == .)) and
  (.MetricDataResults | map(.Timestamps[]) | all((fromdateiso8601? != null)))
' "$pending_file" >/dev/null || {
	echo '{"state":"unknown"}'
	exit 0
}

jq -n \
	--arg now "$now" \
	--argjson desired "$desired" \
	--slurpfile tasks "$tasks_file" \
	--slurpfile terminal "$terminal_file" \
	--slurpfile pending "$pending_file" '
  def values($document;$id): [$document.MetricDataResults[] | select(.Id == $id) | .Values[]];
  def total($id): (values($terminal[0];$id) | add // 0);
  def latest($id):
    ($pending[0].MetricDataResults[] | select(.Id == $id)) as $series |
    {value:$series.Values[0],timestamp:$series.Timestamps[0]};
  def terminal($key;$pipeline;$signal): {
    pipeline:$pipeline,
    signal:$signal,
    sent_item_count:total("sent_" + $key),
    permanently_dropped_item_count:total("drop_" + $key),
    refused_item_count:total("refused_" + $key)
  };
  def pending_for($task;$index;$key;$pipeline;$signal):
    latest("pending_" + $key + "_" + ($index|tostring)) as $count |
    latest("age_" + $key + "_" + ($index|tostring)) as $age |
    if $count.timestamp != $age.timestamp then error("conflicting pending timestamps") else {
      pipeline:$pipeline,
      signal:$signal,
      collector_task_id:$task,
      pending_item_count:$count.value,
      oldest_pending_item_age_seconds:$age.value,
      sampled_at:$age.timestamp
    } end;
  {
    now:$now,
    desired_task_count:$desired,
    running_collector_task_ids:$tasks[0],
    task_list_failed:false,
    terminal:[
      terminal("traces";"traces/app";"traces"),
      terminal("metrics";"metrics/app";"metrics"),
      terminal("logs";"logs/app";"logs")
    ],
    pending:[$tasks[0] | to_entries[] as $task |
      pending_for($task.value;$task.key;"traces";"traces/app";"traces"),
      pending_for($task.value;$task.key;"metrics";"metrics/app";"metrics"),
      pending_for($task.value;$task.key;"logs";"logs/app";"logs")
    ],
    delivery_uncertain:(total("delivery_uncertain") > 0),
    accounting_uncertain:(total("accounting_uncertain") > 0)
  }
' >"$input_file" || {
	echo '{"state":"unknown"}'
	exit 0
}

go run ./cmd/observability-gate loss <"$input_file"
