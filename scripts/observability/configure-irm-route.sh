#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/evidence.sh"

environment="${ACCOUNTABLE_ENVIRONMENT:?set ACCOUNTABLE_ENVIRONMENT}"
capacity_evidence="${ACCOUNTABLE_CAPACITY_EVIDENCE:?set ACCOUNTABLE_CAPACITY_EVIDENCE}"
critical_topic_arn="${ACCOUNTABLE_CRITICAL_TOPIC_ARN:?set ACCOUNTABLE_CRITICAL_TOPIC_ARN}"
region="${AWS_REGION:?set AWS_REGION}"
endpoint_parameter="/accountable/observability/$environment/irm-sns-endpoint"
subscription_parameter="/accountable/observability/$environment/irm-sns-subscription-arn"

case "$environment" in
	development | staging | production) ;;
	*) echo "the IRM environment is invalid" >&2; exit 2 ;;
esac

accountable_require_fresh_capacity "$capacity_evidence"

subscriptions="$(mktemp "${TMPDIR:-/tmp}/accountable-irm-subscriptions.XXXXXX")"
trap 'rm -f "$subscriptions"' EXIT
chmod 600 "$subscriptions"

load_subscriptions() {
	aws sns list-subscriptions-by-topic \
		--region "$region" \
		--topic-arn "$critical_topic_arn" \
		--output json >"$subscriptions"
}

disable_irm_route() {
	local subscription
	load_subscriptions
	while IFS= read -r subscription; do
		aws sns unsubscribe --region "$region" --subscription-arn "$subscription" >/dev/null
	done < <(jq -r '.Subscriptions[] | select(.Protocol == "https" and (.SubscriptionArn | startswith("arn:aws:sns:"))) | .SubscriptionArn' "$subscriptions")
	load_subscriptions
	if jq -e '[.Subscriptions[] | select(.Protocol == "https")] | length > 0' "$subscriptions" >/dev/null; then
		echo "an unconfirmed or duplicate IRM subscription could not be removed" >&2
		return 1
	fi
	aws ssm delete-parameter --region "$region" --name "$subscription_parameter" >/dev/null 2>&1 || true
	echo "IRM routing is disabled for $environment"
}

if [[ "$(jq -r .irm_included_at_no_cost "$capacity_evidence")" != "true" ]]; then
	disable_irm_route
	exit 0
fi

endpoint="$(aws ssm get-parameter \
	--region "$region" \
	--name "$endpoint_parameter" \
	--with-decryption \
	--query 'Parameter.Value' \
	--output text)"
if [[ ! "$endpoint" =~ ^https://[^[:space:]]+$ ]]; then
	echo "the protected IRM Amazon SNS endpoint is invalid" >&2
	exit 2
fi

validate_subscription_set() {
	local matching foreign
	matching="$(jq --arg endpoint "$endpoint" '[.Subscriptions[] | select(.Protocol == "https" and .Endpoint == $endpoint)] | length' "$subscriptions")"
	foreign="$(jq --arg endpoint "$endpoint" '[.Subscriptions[] | select(.Protocol == "https" and .Endpoint != $endpoint)] | length' "$subscriptions")"
	if ((foreign > 0 || matching > 1)); then
		echo "the critical topic has a conflicting IRM subscription set" >&2
		return 1
	fi
}

confirmed_subscription() {
	jq -r --arg endpoint "$endpoint" '
    [.Subscriptions[] | select(.Protocol == "https" and .Endpoint == $endpoint and (.SubscriptionArn | startswith("arn:aws:sns:")))] |
    if length == 1 then .[0].SubscriptionArn else empty end
  ' "$subscriptions"
}

load_subscriptions
validate_subscription_set
subscription="$(confirmed_subscription)"
matching="$(jq --arg endpoint "$endpoint" '[.Subscriptions[] | select(.Protocol == "https" and .Endpoint == $endpoint)] | length' "$subscriptions")"
if [[ -z "$subscription" && "$matching" == "0" ]]; then
	aws sns subscribe \
		--region "$region" \
		--topic-arn "$critical_topic_arn" \
		--protocol https \
		--notification-endpoint "$endpoint" \
		--return-subscription-arn >/dev/null
	matching=1
	load_subscriptions
	validate_subscription_set
	matching="$(jq --arg endpoint "$endpoint" '[.Subscriptions[] | select(.Protocol == "https" and .Endpoint == $endpoint)] | length' "$subscriptions")"
fi
if [[ -z "$subscription" ]]; then
	for _ in $(seq 1 60); do
		load_subscriptions
		validate_subscription_set
		subscription="$(confirmed_subscription)"
		if [[ -n "$subscription" ]]; then
			break
		fi
		sleep 10
	done
fi
unset endpoint

if [[ ! "$subscription" =~ ^arn:aws:sns: ]]; then
	echo "Grafana IRM did not confirm the Amazon SNS subscription" >&2
	exit 1
fi

aws ssm put-parameter \
	--region "$region" \
	--name "$subscription_parameter" \
	--type SecureString \
	--key-id "alias/accountable-$environment-observability" \
	--value "$subscription" \
	--overwrite >/dev/null

echo "IRM routing is confirmed for $environment"
