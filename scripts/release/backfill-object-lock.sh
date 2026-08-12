#!/usr/bin/env bash
set -euo pipefail

bucket="${ACCOUNTABLE_RELEASE_BUCKET:?ACCOUNTABLE_RELEASE_BUCKET is required}"
expected_owner="${ACCOUNTABLE_RELEASE_ACCOUNT_ID:?ACCOUNTABLE_RELEASE_ACCOUNT_ID is required}"

case "$bucket" in
	"accountable-releases-${expected_owner}-"*) ;;
	*)
		echo "release bucket does not match the expected account" >&2
		exit 1
		;;
esac

page=""

seven_years_after() {
	local instant="${1/%+00:00/Z}"
	date -u -d "$instant +7 years" '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null ||
		date -j -u -v+7y -f '%Y-%m-%dT%H:%M:%SZ' "$instant" '+%Y-%m-%dT%H:%M:%SZ'
}

while :; do
	arguments=(
		s3api list-object-versions
		--bucket "$bucket"
		--expected-bucket-owner "$expected_owner"
		--output json
	)
	if [[ -n "$page" ]]; then
		arguments+=(--key-marker "$(jq -r '.NextKeyMarker' <<<"$page")")
		arguments+=(--version-id-marker "$(jq -r '.NextVersionIdMarker' <<<"$page")")
	fi
	page="$(aws "${arguments[@]}")"
	while IFS=$'\t' read -r key version_id last_modified; do
		case "$key" in
			releases/v1/*|observability/v1/cell-inventory/*|observability/v1/promotion-evidence/*) ;;
			*) continue ;;
		esac
		required_retention="$(seven_years_after "$last_modified")"
		retention="$(aws s3api get-object-retention \
			--bucket "$bucket" \
			--key "$key" \
			--version-id "$version_id" \
			--expected-bucket-owner "$expected_owner" \
			--output json 2>/dev/null || true)"
		mode="$(jq -r '.Retention.Mode // ""' <<<"$retention")"
		retained_until="$(jq -r '.Retention.RetainUntilDate // ""' <<<"$retention")"
		retained_until="${retained_until/%+00:00/Z}"
		if [[ "$mode" == "COMPLIANCE" && -n "$retained_until" ]] && [[ ! "$retained_until" < "$required_retention" ]]; then
			continue
		fi
		aws s3api put-object-retention \
			--bucket "$bucket" \
			--key "$key" \
			--version-id "$version_id" \
			--expected-bucket-owner "$expected_owner" \
			--retention "Mode=COMPLIANCE,RetainUntilDate=$required_retention"
	done < <(jq -r '.Versions[]? | [.Key, .VersionId, .LastModified] | @tsv' <<<"$page")
	if [[ "$(jq -r '.IsTruncated' <<<"$page")" != "true" ]]; then
		break
	fi
done
