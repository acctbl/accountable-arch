#!/usr/bin/env bash
set -euo pipefail

temporary="$(mktemp -d "${TMPDIR:-/tmp}/accountable-retention-test.XXXXXX")"
trap 'rm -rf "$temporary"' EXIT
ln -s "$(pwd)/testdata/release/fake-aws-retention.sh" "$temporary/aws"
log="$temporary/aws.log"

ACCOUNTABLE_FAKE_AWS_LOG="$log" \
ACCOUNTABLE_FAKE_RETAIN_UNTIL="2026-01-02T03:04:05Z" \
ACCOUNTABLE_RELEASE_BUCKET="accountable-releases-453722413624-eu-west-2" \
ACCOUNTABLE_RELEASE_ACCOUNT_ID="453722413624" \
PATH="$temporary:$PATH" \
	bash scripts/release/backfill-object-lock.sh

rg -F 'Mode=COMPLIANCE,RetainUntilDate=2027-01-02T03:04:05Z' "$log" >/dev/null

: >"$log"
ACCOUNTABLE_FAKE_AWS_LOG="$log" \
ACCOUNTABLE_FAKE_RETAIN_UNTIL="2027-01-02T03:04:05Z" \
ACCOUNTABLE_RELEASE_BUCKET="accountable-releases-453722413624-eu-west-2" \
ACCOUNTABLE_RELEASE_ACCOUNT_ID="453722413624" \
PATH="$temporary:$PATH" \
	bash scripts/release/backfill-object-lock.sh

if [[ -s "$log" ]]; then
	echo "a sufficiently retained object was changed" >&2
	exit 1
fi
