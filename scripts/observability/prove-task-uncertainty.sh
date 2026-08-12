#!/usr/bin/env bash
set -euo pipefail

environment="${ACCOUNTABLE_ENVIRONMENT:?set ACCOUNTABLE_ENVIRONMENT}"
cell="${ACCOUNTABLE_CELL:?set ACCOUNTABLE_CELL}"
cell_output="${ACCOUNTABLE_CELL_OUTPUT:?set ACCOUNTABLE_CELL_OUTPUT}"

cluster="$(jq -er '.ecs_cluster_arn.value' "$cell_output")"
collector_service="$(jq -er '.collector_service_name.value' "$cell_output")"
api_service="$(jq -er '.api_service_name.value' "$cell_output")"

minutes_ago() {
	date -u -d "$1 minutes ago" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null ||
		date -u -v-"$1"M +%Y-%m-%dT%H:%M:%SZ
}

metric_sum() {
	local namespace="$1" metric="$2"
	shift 2
	aws cloudwatch get-metric-statistics \
		--namespace "$namespace" \
		--metric-name "$metric" \
		--dimensions "$@" \
		--start-time "$(minutes_ago 30)" \
		--end-time "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
		--period 60 \
		--statistics Sum \
		--output json |
		jq -r '[.Datapoints[].Sum] | add // 0'
}

sli_uncertainty() {
	metric_sum Accountable/Collector SLIDeliveryUncertain \
		"Name=Environment,Value=$environment" \
		"Name=CellId,Value=$cell" \
		"Name=Source,Value=$1"
}

collector_uncertainty() {
	metric_sum Accountable/Collector CollectorDeliveryUncertain \
		"Name=Environment,Value=$environment" \
		"Name=CellId,Value=$cell"
}

client_uncertainty() {
	metric_sum Accountable/ClientTelemetry ClientEventDeliveryUncertain \
		"Name=Environment,Value=$environment" \
		"Name=CellId,Value=$cell"
}

running_task() {
	aws ecs list-tasks \
		--cluster "$cluster" \
		--service-name "$1" \
		--desired-status RUNNING \
		--query 'taskArns[0]' \
		--output text
}

stop_and_replace() {
	local service="$1" reason="$2" previous current
	previous="$(running_task "$service")"
	if [[ ! "$previous" =~ ^arn:aws:ecs: ]]; then
		echo "the $service service has no running task" >&2
		exit 1
	fi
	aws ecs stop-task --cluster "$cluster" --task "$previous" --reason "$reason" >/dev/null
	aws ecs wait services-stable --cluster "$cluster" --services "$service"
	current="$(running_task "$service")"
	if [[ ! "$current" =~ ^arn:aws:ecs: || "$current" == "$previous" ]]; then
		echo "the $service task was not replaced" >&2
		exit 1
	fi
}

collector_sli_before="$(sli_uncertainty collector_task)"
collector_before="$(collector_uncertainty)"
stop_and_replace "$collector_service" "accountable managed clean replacement proof"
sleep 60
if [[ "$(sli_uncertainty collector_task)" != "$collector_sli_before" || "$(collector_uncertainty)" != "$collector_before" ]]; then
	echo "a clean Collector replacement created uncertainty" >&2
	exit 1
fi

api_sli_before="$(sli_uncertainty api_task)"
client_before="$(client_uncertainty)"
stop_and_replace "$collector_service" "accountable managed unexpected-stop proof"
stop_and_replace "$api_service" "accountable managed unexpected-stop proof"

for _ in $(seq 1 18); do
	if jq -en \
		--argjson collector_sli "$(sli_uncertainty collector_task)" \
		--argjson collector_sli_before "$collector_sli_before" \
		--argjson collector "$(collector_uncertainty)" \
		--argjson collector_before "$collector_before" \
		--argjson api_sli "$(sli_uncertainty api_task)" \
		--argjson api_sli_before "$api_sli_before" \
		--argjson client "$(client_uncertainty)" \
		--argjson client_before "$client_before" '
      $collector_sli > $collector_sli_before and
      $collector > $collector_before and
      $api_sli > $api_sli_before and
      $client > $client_before
    ' >/dev/null; then
		echo "task-uncertainty-proof: pass"
		exit 0
	fi
	sleep 10
done

echo "the declared unexpected task stops did not create every uncertainty metric" >&2
exit 1
