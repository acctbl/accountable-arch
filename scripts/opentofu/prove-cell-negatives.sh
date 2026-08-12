#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
source_root="$repo_root/infra/opentofu"
fixture_root="$repo_root/testdata/opentofu/cell-negative"
expected_account_id="453722413624"
provider_cache="${TMPDIR:-/tmp}/accountable-opentofu-provider-cache"
mkdir -p "$provider_cache"
export TF_PLUGIN_CACHE_DIR="$provider_cache"

proof_root="$(mktemp -d "${TMPDIR:-/tmp}/accountable-cell-negatives.XXXXXX")"
mock_pid=""
cleanup() {
	if [[ -n "$mock_pid" ]] && kill -0 "$mock_pid" 2>/dev/null; then
		kill "$mock_pid"
		wait "$mock_pid" 2>/dev/null || true
	fi
	rm -rf "$proof_root"
}
trap cleanup EXIT

# Exercise the checked-in development root and its real local cell module. The
# backend override keeps the proof offline, and static test credentials ensure
# an ambient developer or CI identity can never be used by this test.
proof_source="$proof_root/opentofu"
mkdir -p "$proof_source"
tar -C "$source_root" \
	--exclude=.terraform \
	--exclude='*/.terraform' \
	--exclude='*.tfstate' \
	--exclude='*.tfstate.*' \
	-cf - . | tar -C "$proof_source" -xf -

cell_root="$proof_source/environments/development/cells/development-01"
module_root="$proof_source/modules/cell"
cp "$proof_source/.terraform.lock.hcl" "$cell_root/.terraform.lock.hcl"

unset AWS_PROFILE AWS_ROLE_ARN AWS_WEB_IDENTITY_TOKEN_FILE AWS_SESSION_TOKEN AWS_SECURITY_TOKEN
export AWS_ACCESS_KEY_ID="offline-test"
export AWS_SECRET_ACCESS_KEY="offline-test"
export AWS_EC2_METADATA_DISABLED="true"
export AWS_MAX_ATTEMPTS="1"
export TF_DATA_DIR="$proof_root/.terraform-data"
export GOCACHE="$proof_root/go-build-cache"

(cd "$repo_root" && go build -buildvcs=false -o "$proof_root/tofupolicy" ./cmd/tofupolicy)
(cd "$repo_root" && go build -buildvcs=false -o "$proof_root/aws-account-mock" ./scripts/opentofu/aws-account-mock)
endpoint_file="$proof_root/aws-account-endpoint"
account_file="$proof_root/aws-account-id"
printf '%s' "$expected_account_id" >"$account_file"
"$proof_root/aws-account-mock" --account-file "$account_file" --endpoint-file "$endpoint_file" &
mock_pid=$!
for _ in {1..100}; do
	if [[ -s "$endpoint_file" ]]; then break; fi
	if ! kill -0 "$mock_pid" 2>/dev/null; then
		echo "offline AWS account mock exited before becoming ready" >&2
		exit 1
	fi
	sleep 0.05
done
if [[ ! -s "$endpoint_file" ]]; then
	echo "offline AWS account mock did not become ready" >&2
	exit 1
fi
offline_endpoint="$(<"$endpoint_file")"
sed "s|OFFLINE_AWS_ENDPOINT|$offline_endpoint|g" "$fixture_root/offline-root.tf" >"$cell_root/offline_override.tf"

# The module's postconditions are the first safety layer and correctly prevent
# an unsafe plan from being saved. Remove them only in this temporary copy so
# the independent saved-plan policy layer can be exercised with a real unsafe
# binary plan. The safe baseline below proves this transformation does not
# itself manufacture a policy violation.
strip_lifecycle_guards() {
	local file
	while IFS= read -r -d '' file; do
		if ! grep -F -q -- '  lifecycle {' "$file"; then
			continue
		fi
		awk '
			!skipping && /^  lifecycle \{$/ { skipping = 1; depth = 1; next }
			skipping {
				line = $0
				opens = gsub(/\{/, "{", line)
				closes = gsub(/\}/, "}", line)
				depth += opens - closes
				if (depth == 0) skipping = 0
				next
			}
			{ print }
		' "$file" >"$file.without-lifecycle"
		mv "$file.without-lifecycle" "$file"
	done < <(find "$module_root" -type f -name '*.tf' -print0)
}

strip_lifecycle_guards

# refresh=false does not suppress reads for new data sources. Replace the
# managed prefix-list result with a valid static test ID, then remove both
# AWS-backed data blocks from the temporary copy. Provider-local IAM policy
# document data sources remain intact.
sed 's/data\.aws_ec2_managed_prefix_list\.cloudfront\.id/"pl-12345678"/' \
	"$module_root/security.tf" >"$module_root/security.tf.offline"
mv "$module_root/security.tf.offline" "$module_root/security.tf"

remove_hcl_block() {
	local file="$1"
	local header="$2"
	awk -v header="$header" '
		!skipping && $0 ~ header {
			line = $0
			depth = gsub(/\{/, "{", line) - gsub(/\}/, "}", line)
			if (depth > 0) skipping = 1
			next
		}
		skipping {
			line = $0
			depth += gsub(/\{/, "{", line) - gsub(/\}/, "}", line)
			if (depth == 0) skipping = 0
			next
		}
		{ print }
	' "$file" >"$file.offline"
	mv "$file.offline" "$file"
}

remove_hcl_block "$module_root/locals.tf" '^data "aws_caller_identity" "current"'
remove_hcl_block "$module_root/security.tf" '^data "aws_ec2_managed_prefix_list" "cloudfront"'

tofu -chdir="$cell_root" init -input=false -lockfile=readonly >/dev/null

plan_args=(
	-input=false
	-lock=false
	-refresh=false
	-var="api_desired_count=1"
	-var="configuration_revision=negative-proof"
	-var="image_uri=453722413624.dkr.ecr.eu-west-2.amazonaws.com/accountable@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	-var="collector_image_uri=453722413624.dkr.ecr.eu-west-2.amazonaws.com/accountable-collector@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	-var="grafana_otlp_endpoint=https://otlp-gateway-prod-gb-south-1.grafana.net/otlp"
	-var="runbook_base_url=https://github.com/acctbl/accountable/tree/main/docs/runbooks"
	-var="dashboard_base_url=https://acctbl.grafana.net"
	-var="release_id=018f47d2-7f00-7abc-8def-1234567890ab"
	-var="release_manifest_digest=sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	-var="web_asset_digest=sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
)

save_plan() {
	local name="$1"
	local plan_file="$proof_root/$name.plan"
	local plan_log="$proof_root/$name-plan.log"
	local plan_json="$proof_root/$name-plan.json"

	if ! tofu -chdir="$cell_root" plan "${plan_args[@]}" -out="$plan_file" >"$plan_log" 2>&1; then
		cat "$plan_log" >&2
		echo "negative cell proof could not create saved plan: $name" >&2
		return 1
	fi
	if [[ ! -s "$plan_file" ]]; then
		echo "negative cell proof did not create a binary saved plan: $name" >&2
		return 1
	fi
	tofu -chdir="$cell_root" show -json "$plan_file" >"$plan_json"
	printf '%s\n' "$plan_json"
}

clear_mutation() {
	rm -f "$cell_root/unsafe_override.tf" "$module_root/unsafe_override.tf"
}

clear_mutation
safe_plan_json="$(save_plan safe-baseline)"
"$proof_root/tofupolicy" --account-id "$expected_account_id" --plan "$safe_plan_json" >/dev/null
echo "saved-plan policy baseline passed"

prove() {
	local name="$1"
	local location="$2"
	local expected="$3"
	local policy_log="$proof_root/$name-policy.log"
	local plan_json

	clear_mutation
	cp "$fixture_root/$name.tf" "$location/unsafe_override.tf"
	if [[ "$name" == "wrong-account" ]]; then
		printf '%s' "000000000000" >"$account_file"
	else
		printf '%s' "$expected_account_id" >"$account_file"
	fi
	plan_json="$(save_plan "$name")"
	if "$proof_root/tofupolicy" --account-id "$expected_account_id" --plan "$plan_json" >"$policy_log" 2>&1; then
		echo "saved-plan policy unexpectedly accepted negative cell plan: $name" >&2
		return 1
	fi
	if ! grep -F -q -- "$expected" "$policy_log"; then
		cat "$policy_log" >&2
		echo "saved-plan policy rejected the wrong condition: $name" >&2
		return 1
	fi
	echo "saved-plan policy rejected real cell mutation: $name"
}

prove wrong-account "$cell_root" "AWS provider account allow-list does not match the expected account"
prove public-rds "$module_root" "RDS publicly_accessible must be false"
prove single-az-rds "$module_root" "RDS must be Multi-AZ"
prove internet-facing-alb "$module_root" "ALB must be internal"
prove public-ecs "$module_root" "ECS service must not assign a public IP"
prove weak-s3 "$module_root" "S3 public-access controls must all be true"
prove world-open-ingress "$module_root" "security-group ingress is world-open"
prove mutable-image "$module_root" "every task image must use a sha256 digest"
prove malformed-image-digest "$module_root" "every task image must use a sha256 digest"
prove missing-waf "$module_root" "CloudFront must have an AWS WAF web ACL"
prove tmpfs-scratch "$module_root" "Fargate tasks must not use linuxParameters.tmpfs"
prove host-path-scratch "$module_root" "every container mount must be the task-scoped runtime scratch volume"

clear_mutation
