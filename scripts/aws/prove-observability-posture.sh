#!/usr/bin/env bash
set -euo pipefail

# Prove the live observability posture of one cell and its environment. Run it
# after any change to a cell, the alert routes, or the scheduled producers.
#
# Every check reads live AWS state; none infers success from an apply.

cell="${ACCOUNTABLE_CELL:?set ACCOUNTABLE_CELL}"
release_bucket="${ACCOUNTABLE_RELEASE_BUCKET:?set ACCOUNTABLE_RELEASE_BUCKET}"
environment="${ACCOUNTABLE_ENVIRONMENT:-development}"
region="${AWS_REGION:-eu-west-2}"

cluster="accountable-$cell"
observability="accountable-$environment-observability"
account="$(aws sts get-caller-identity --query Account --output text)"
topic_prefix="arn:aws:sns:$region:$account:$observability"

minutes_ago() {
	date -u -d "$1 minutes ago" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null ||
		date -u -v-"$1"M +%Y-%m-%dT%H:%M:%SZ
}

metric_sum() {
	aws cloudwatch get-metric-statistics \
		--namespace "$1" --metric-name "$2" --dimensions "$3" \
		--start-time "$(minutes_ago "$4")" --end-time "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
		--period 60 --statistics Sum --output json |
		jq -r '[.Datapoints[].Sum] | add // 0'
}

# The Collector task mounts the task-scoped scratch volume, writable only by its
# init container, on encrypted ephemeral storage, and never through tmpfs.
aws ecs describe-task-definition --task-definition "$cluster-collector" --output json |
	jq -e '
		.taskDefinition
		| (.volumes | length == 1 and (.[0].host.sourcePath // null) == null)
		and (.containerDefinitions | all(.linuxParameters.tmpfs == null))
		and (.containerDefinitions[] | select(.name == "collector-init")
			| any(.mountPoints[]; .containerPath == "/run/accountable" and .readOnly == false))
		and (.containerDefinitions[] | select(.name == "collector")
			| any(.mountPoints[]; .containerPath == "/run/accountable" and .readOnly == true))
	' >/dev/null

aws ecs describe-clusters --clusters "$cluster" --include CONFIGURATIONS --output json |
	jq -e '.clusters[0].configuration.managedStorageConfiguration.fargateEphemeralStorageKmsKeyId | strings | select(length > 0)' >/dev/null

aws ecs describe-services --cluster "$cluster" --services "$cluster-collector" --output json |
	jq -e '.services[0] | .desiredCount > 0 and .runningCount == .desiredCount' >/dev/null

# The active name must resolve.
# A standby name exists only during an overlap-first replacement.
aws ssm get-parameter --name "/accountable/$cell/$environment/collector/grafana-otlp-token-active" \
	--query 'Parameter.Name' --output text >/dev/null

go run ./cmd/cell-inventory read \
	--bucket "$release_bucket" --region "$region" \
	--environment "$environment" --cell-id "$cell" |
	jq -e 'length > 0 and (.[-1].state == "active")' >/dev/null

# The canary must have reported success recently, from outside Grafana.
aws cloudwatch get-metric-statistics \
	--namespace Accountable/Observability --metric-name CanarySuccess \
	--dimensions Name=StackName,Value=acctbl "Name=Environment,Value=$environment" \
	--start-time "$(minutes_ago 20)" --end-time "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
	--period 300 --statistics Minimum --output json |
	jq -e '.Datapoints | length > 0 and (sort_by(.Timestamp) | last | .Minimum == 1)' >/dev/null

# Flipping the canary alarm proves the whole route: alarm to input topic, to
# translator, to output topic. Its next evaluation restores the real state, so
# nothing is left behind.
before="$(metric_sum AWS/Lambda Invocations "Name=FunctionName,Value=$observability-translator" 10)"
aws cloudwatch set-alarm-state --alarm-name "$observability-canary" \
	--state-value ALARM --state-reason "observability posture proof"
sleep 45
aws cloudwatch set-alarm-state --alarm-name "$observability-canary" \
	--state-value OK --state-reason "observability posture recovery"
sleep 15

after="$(metric_sum AWS/Lambda Invocations "Name=FunctionName,Value=$observability-translator" 10)"
errors="$(metric_sum AWS/Lambda Errors "Name=FunctionName,Value=$observability-translator" 10)"
if [[ "$after" == "$before" ]]; then
	echo "the alarm did not reach the translator" >&2
	exit 1
fi
if [[ "$errors" != "0" ]]; then
	echo "the translator reported $errors errors" >&2
	exit 1
fi

# The direct failure route must reach a person without the translator.
aws sns list-subscriptions-by-topic --topic-arn "$topic_prefix-translator-failure" --output json |
	jq -e '[.Subscriptions[] | select(.Protocol == "email" and .SubscriptionArn != "PendingConfirmation")] | length > 0' >/dev/null

aws cloudwatch describe-alarms --alarm-name-prefix "$observability-translator-" --output json |
	jq -e --arg topic "$topic_prefix-translator-failure" \
		'.MetricAlarms | length > 0 and all(.AlarmActions == [$topic])' >/dev/null

echo "observability-posture: pass"
