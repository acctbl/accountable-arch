#!/usr/bin/env bash
set -euo pipefail

environment="${ACCOUNTABLE_ENVIRONMENT:?set ACCOUNTABLE_ENVIRONMENT}"
region="${AWS_REGION:?set AWS_REGION}"
observed_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
cell_output="$(mktemp "${TMPDIR:-/tmp}/accountable-cell-output.XXXXXX")"
ecs_output="$(mktemp "${TMPDIR:-/tmp}/accountable-ecs.XXXXXX")"
rds_output="$(mktemp "${TMPDIR:-/tmp}/accountable-rds.XXXXXX")"
target_output="$(mktemp "${TMPDIR:-/tmp}/accountable-targets.XXXXXX")"
cloudfront_output="$(mktemp "${TMPDIR:-/tmp}/accountable-cloudfront.XXXXXX")"
trap 'rm -f "$cell_output" "$ecs_output" "$rds_output" "$target_output" "$cloudfront_output"' EXIT
cat >"$cell_output"

unknown() {
	jq -n --arg observed_at "$observed_at" '{state:"unknown",observed_at:$observed_at}'
	exit 0
}

jq -e '
  [.ecs_cluster_arn,.ecs_service_name,.collector_service_name,.rds_instance_identifier,.api_target_group_arn,.cloudfront_distribution_id] |
  all(.value | type == "string" and length > 0)
' "$cell_output" >/dev/null || unknown

cluster="$(jq -er .ecs_cluster_arn.value "$cell_output")"
api="$(jq -er .ecs_service_name.value "$cell_output")"
collector="$(jq -er .collector_service_name.value "$cell_output")"
rds="$(jq -er .rds_instance_identifier.value "$cell_output")"
target_group="$(jq -er .api_target_group_arn.value "$cell_output")"
distribution="$(jq -er .cloudfront_distribution_id.value "$cell_output")"

aws ecs describe-services --region "$region" --cluster "$cluster" --services "$api" "$collector" --output json >"$ecs_output" || unknown
aws rds describe-db-instances --region "$region" --db-instance-identifier "$rds" --output json >"$rds_output" || unknown
aws elbv2 describe-target-health --region "$region" --target-group-arn "$target_group" --output json >"$target_output" || unknown
aws cloudfront get-distribution --id "$distribution" --output json >"$cloudfront_output" || unknown

expected=1
if [[ "$environment" != "development" ]]; then expected=2; fi
if ! jq -e --argjson expected "$expected" '
  (.services | length) == 2 and
  (.failures | length) == 0 and
  (.services | all(
    .desiredCount == $expected and .runningCount == $expected and .pendingCount == 0 and
    (.deployments | length) == 1 and .deployments[0].status == "PRIMARY" and .deployments[0].rolloutState == "COMPLETED"
  ))
' "$ecs_output" >/dev/null; then
	jq -n --arg observed_at "$observed_at" '{state:"fail",observed_at:$observed_at}'
	exit 0
fi

if ! jq -e --arg environment "$environment" '
  .DBInstances[0] as $db |
  $db.DBInstanceStatus == "available" and
  $db.MultiAZ == true and
  $db.StorageEncrypted == true and
  ($environment == "development" or $db.DeletionProtection == true)
' "$rds_output" >/dev/null; then
	jq -n --arg observed_at "$observed_at" '{state:"fail",observed_at:$observed_at}'
	exit 0
fi

if ! jq -e --argjson expected "$expected" '
  (.TargetHealthDescriptions | length) == $expected and
  (.TargetHealthDescriptions | all(.TargetHealth.State == "healthy"))
' "$target_output" >/dev/null; then
	jq -n --arg observed_at "$observed_at" '{state:"fail",observed_at:$observed_at}'
	exit 0
fi

if ! jq -e '.Distribution.Status == "Deployed" and .Distribution.DistributionConfig.Enabled == true' "$cloudfront_output" >/dev/null; then
	jq -n --arg observed_at "$observed_at" '{state:"fail",observed_at:$observed_at}'
	exit 0
fi

jq -n --arg observed_at "$observed_at" '{state:"pass",observed_at:$observed_at}'
