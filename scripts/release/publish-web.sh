#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

release_bucket="${RELEASE_BUCKET:?set RELEASE_BUCKET}"
web_bucket="${WEB_BUCKET:?set WEB_BUCKET}"
manifest_digest="${MANIFEST_DIGEST:?set MANIFEST_DIGEST}"
bundle_digest="${BUNDLE_DIGEST:?set BUNDLE_DIGEST}"
workflow_identity="${WORKFLOW_IDENTITY:?set WORKFLOW_IDENTITY}"
runtime_config_base64="${RUNTIME_CONFIG_BASE64:?set RUNTIME_CONFIG_BASE64}"

work="$(mktemp -d "${TMPDIR:-/tmp}/accountable-web.XXXXXX")"
trap 'rm -rf "$work"' EXIT

aws s3api get-object --bucket "$release_bucket" \
	--key "releases/v1/manifests/$manifest_digest.json" "$work/release.json" >/dev/null
aws s3api get-object --bucket "$release_bucket" \
	--key "releases/v1/bundles/$bundle_digest.json" "$work/release.sigstore.json" >/dev/null

jq '[.artifacts[] | {key: .name, value: .digest}] | from_entries' \
	"$work/release.json" >"$work/artifacts.json"
go run ./cmd/release verify \
	--manifest "$work/release.json" \
	--bundle "$work/release.sigstore.json" \
	--manifest-digest "$manifest_digest" \
	--bundle-digest "$bundle_digest" \
	--artifacts "$work/artifacts.json" \
	--workflow-identity "$workflow_identity" >/dev/null

release_id="$(jq -r '.release_id' "$work/release.json")"
web_digest="$(jq -r '.artifacts[] | select(.name == "web_assets") | .digest' "$work/release.json")"
archive_digest="$(jq -r '.artifacts[] | select(.name == "web_assets") | .archive_digest' "$work/release.json")"

aws s3api get-object --bucket "$release_bucket" \
	--key "releases/v1/web/$archive_digest.tar.gz" "$work/web-assets.tar.gz" >/dev/null
aws s3api get-object --bucket "$release_bucket" \
	--key "releases/v1/web/$web_digest.json" "$work/web-manifest.json" >/dev/null

actual_archive_digest="sha256:$(sha256sum "$work/web-assets.tar.gz" | cut -d' ' -f1)"
if [[ "$actual_archive_digest" != "$archive_digest" ]]; then
	echo "the web archive does not match the signed release" >&2
	exit 1
fi

go run ./cmd/release extract-web \
	--archive "$work/web-assets.tar.gz" \
	--root "$work/dist"

go run ./cmd/release verify-web \
	--root "$work/dist" \
	--manifest "$work/web-manifest.json" \
	--digest "$web_digest"

mkdir -p "$work/dist/_runtime"
printf '%s' "$runtime_config_base64" | base64 -d >"$work/dist/_runtime/config.json"
go run ./cmd/release web-release \
	--release-id "$release_id" \
	--manifest-digest "$manifest_digest" \
	--web-digest "$web_digest" \
	--output "$work/dist/_runtime/release.json"

aws s3 sync "$work/dist" "s3://$web_bucket" --delete --only-show-errors
