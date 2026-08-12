#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/evidence.sh"

environment="${ACCOUNTABLE_ENVIRONMENT:?set ACCOUNTABLE_ENVIRONMENT}"
capacity_evidence="${ACCOUNTABLE_CAPACITY_EVIDENCE:?set ACCOUNTABLE_CAPACITY_EVIDENCE}"
stack_url="${ACCOUNTABLE_GRAFANA_STACK_URL:?set ACCOUNTABLE_GRAFANA_STACK_URL}"
irm_api_url="${ACCOUNTABLE_IRM_API_URL:-}"
region="${AWS_REGION:?set AWS_REGION}"

case "$environment" in
	development | staging | production) ;;
	*) echo "the paging environment is invalid" >&2; exit 2 ;;
esac

accountable_require_fresh_capacity "$capacity_evidence"

account="$(aws sts get-caller-identity --query Account --output text)"
prefix="accountable-$environment-observability"
input_topic="arn:aws:sns:$region:$account:$prefix-alarm-input"
critical_topic="arn:aws:sns:$region:$account:$prefix-critical"
topic_name="$prefix-critical"
work="$(mktemp -d "${TMPDIR:-/tmp}/accountable-paging-proof.XXXXXX")"
trap 'rm -rf "$work"' EXIT
chmod 700 "$work"

aws cloudwatch describe-alarms --alarm-name-prefix "accountable-$environment-" --output json >"$work/alarms.json"
alarm_name="$(jq -r --arg input "$input_topic" '
  [
    .MetricAlarms[]
    | select(.StateValue == "OK")
    | select((.AlarmActions // []) | index($input))
    | select((.AlarmDescription | fromjson? | .severity) == "critical")
    | .AlarmName
  ]
  | sort
  | first // empty
' "$work/alarms.json")"
if [[ -z "$alarm_name" ]]; then
	echo "no healthy critical alarm is available for the paging proof" >&2
	exit 1
fi

aws sns list-subscriptions-by-topic --topic-arn "$critical_topic" --output json >"$work/subscriptions.json"
email_subscriptions="$(jq '[.Subscriptions[] | select(.Protocol == "email" and (.SubscriptionArn | startswith("arn:aws:sns:")))] | length' "$work/subscriptions.json")"
if (( email_subscriptions < 1 )); then
	echo "the critical email route is not confirmed" >&2
	exit 1
fi

irm_enabled="$(jq -r .irm_included_at_no_cost "$capacity_evidence")"
https_subscriptions="$(jq '[.Subscriptions[] | select(.Protocol == "https" and (.SubscriptionArn | startswith("arn:aws:sns:")))] | length' "$work/subscriptions.json")"
if [[ "$irm_enabled" == "true" && "$https_subscriptions" != "1" ]]; then
	echo "the entitlement-controlled IRM route is not uniquely confirmed" >&2
	exit 1
fi
if [[ "$irm_enabled" != "true" && "$https_subscriptions" != "0" ]]; then
	echo "an IRM route exists without a verified no-cost entitlement" >&2
	exit 1
fi

confirmed_subscriptions=$((email_subscriptions + https_subscriptions))

topic_metric_sum() {
	local metric="$1" start="$2" end="$3"
	aws cloudwatch get-metric-statistics \
		--namespace AWS/SNS \
		--metric-name "$metric" \
		--dimensions "Name=TopicName,Value=$topic_name" \
		--start-time "$start" \
		--end-time "$end" \
		--period 60 \
		--statistics Sum \
		--output json |
		jq -r '[.Datapoints[].Sum] | add // 0'
}

minutes_ago() {
	date -u -d "$1 minutes ago" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null ||
		date -u -v-"$1"M +%Y-%m-%dT%H:%M:%SZ
}

wait_for_delivery() {
	local start="$1" baseline_delivered="$2" baseline_failed="$3" minimum="$4"
	for _ in $(seq 1 18); do
		local now delivered failed
		now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
		delivered="$(topic_metric_sum NumberOfNotificationsDelivered "$start" "$now")"
		failed="$(topic_metric_sum NumberOfNotificationsFailed "$start" "$now")"
		if jq -en \
			--argjson delivered "$delivered" \
			--argjson failed "$failed" \
			--argjson baseline_delivered "$baseline_delivered" \
			--argjson baseline_failed "$baseline_failed" \
			--argjson minimum "$minimum" \
			'$failed == $baseline_failed and ($delivered - $baseline_delivered) >= $minimum' >/dev/null; then
			return 0
		fi
		sleep 10
	done
	return 1
}

proof_started="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
metric_window_start="$(minutes_ago 15)"
delivered_before="$(topic_metric_sum NumberOfNotificationsDelivered "$metric_window_start" "$proof_started")"
failed_before="$(topic_metric_sum NumberOfNotificationsFailed "$metric_window_start" "$proof_started")"
restore_alarm() {
	aws cloudwatch set-alarm-state \
		--alarm-name "$alarm_name" \
		--state-value OK \
		--state-reason "accountable managed paging proof recovery" >/dev/null 2>&1 || true
}
trap 'restore_alarm; rm -rf "$work"' EXIT

aws cloudwatch set-alarm-state \
	--alarm-name "$alarm_name" \
	--state-value ALARM \
	--state-reason "accountable managed paging proof" >/dev/null

if ! wait_for_delivery "$metric_window_start" "$delivered_before" "$failed_before" "$confirmed_subscriptions"; then
	echo "the critical firing notification did not reach every confirmed route" >&2
	exit 1
fi

if [[ "$irm_enabled" == "true" ]]; then
	if [[ ! "$irm_api_url" =~ ^https://[^[:space:]]+$ ]]; then
		echo "the protected IRM API URL is required for acknowledgement proof" >&2
		exit 2
	fi
	token="$(aws ssm get-parameter \
		--name "/accountable/observability/$environment/irm-api-token" \
		--with-decryption \
		--query 'Parameter.Value' \
		--output text)"
	printf 'header = "Authorization: Bearer %s"\n' "$token" >"$work/curl.conf"
	printf 'header = "X-Grafana-URL: %s"\n' "$stack_url" >>"$work/curl.conf"
	printf 'header = "Content-Type: application/json"\n' >>"$work/curl.conf"
	chmod 600 "$work/curl.conf"
	unset token

	start_filter="${proof_started%Z}"
	for _ in $(seq 1 150); do
		end_filter="$(date -u +%Y-%m-%dT%H:%M:%S)"
		if curl --config "$work/curl.conf" \
			--fail-with-body \
			--silent \
			--show-error \
			--get "${irm_api_url%/}/api/v1/alert_groups/" \
			--data-urlencode "search=$alarm_name" \
			--data-urlencode "started_at=${start_filter}_${end_filter}" \
			--output "$work/alert-groups.json" &&
			jq -e --arg alarm "$alarm_name" '
          (.next == null and .total_pages == 1) and ([
            .results[]
            | select((.title | type) == "string" and (.title | contains($alarm)))
            | select(.state == "acknowledged" or .state == "resolved")
            | select((.acknowledged_at | type) == "string" and (.acknowledged_at | length) > 0)
            | select((.acknowledged_by | type) == "string" and (.acknowledged_by | length) > 0)
          ] | length == 1)
        ' "$work/alert-groups.json" >/dev/null; then
			break
		fi
		sleep 10
	done
	if ! jq -e --arg alarm "$alarm_name" '
    (.next == null and .total_pages == 1) and
    ([.results[] | select((.title | type) == "string" and (.title | contains($alarm))) | select(.acknowledged_at != null and .acknowledged_by != null)] | length == 1)
  ' "$work/alert-groups.json" >/dev/null 2>&1; then
		echo "the IRM alert was not acknowledged by a human within 25 minutes" >&2
		exit 1
	fi
fi

recovery_started="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
metric_window_start="$(minutes_ago 15)"
delivered_before="$(topic_metric_sum NumberOfNotificationsDelivered "$metric_window_start" "$recovery_started")"
failed_before="$(topic_metric_sum NumberOfNotificationsFailed "$metric_window_start" "$recovery_started")"
restore_alarm
if ! wait_for_delivery "$metric_window_start" "$delivered_before" "$failed_before" "$confirmed_subscriptions"; then
	echo "the critical recovery notification did not reach every confirmed route" >&2
	exit 1
fi

echo "paging-proof: pass"
