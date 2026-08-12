#!/usr/bin/env bash
set -euo pipefail

case "$1:$2" in
	s3api:list-object-versions)
		printf '%s\n' '{"Versions":[{"Key":"releases/v1/manifests/sha256:test.json","VersionId":"version-1","LastModified":"2020-01-02T03:04:05Z"}],"IsTruncated":false}'
		;;
	s3api:get-object-retention)
		printf '{"Retention":{"Mode":"COMPLIANCE","RetainUntilDate":"%s"}}\n' "${ACCOUNTABLE_FAKE_RETAIN_UNTIL:?}"
		;;
	s3api:put-object-retention)
		printf '%s\n' "$*" >>"${ACCOUNTABLE_FAKE_AWS_LOG:?}"
		;;
	*)
		echo "unexpected fake AWS call: $*" >&2
		exit 1
		;;
esac
