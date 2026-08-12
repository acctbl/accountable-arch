#!/usr/bin/env bash
set -euo pipefail

# Prove that a deployment may proceed: the release manifest is signed by the
# protected release workflow, both deployed image digests belong to that
# manifest, and each image carries its own signature. Reports the release ID.

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

bucket="${RELEASE_BUCKET:?set RELEASE_BUCKET}"
manifest_digest="${MANIFEST_DIGEST:?set MANIFEST_DIGEST}"
bundle_digest="${BUNDLE_DIGEST:?set BUNDLE_DIGEST}"
image_uri="${IMAGE_URI:?set IMAGE_URI}"
collector_image_uri="${COLLECTOR_IMAGE_URI:?set COLLECTOR_IMAGE_URI}"
workflow_identity="${WORKFLOW_IDENTITY:?set WORKFLOW_IDENTITY}"
api_image_identity="${API_IMAGE_IDENTITY:?set API_IMAGE_IDENTITY}"
collector_image_identity="${COLLECTOR_IMAGE_IDENTITY:?set COLLECTOR_IMAGE_IDENTITY}"

work="$(mktemp -d "${TMPDIR:-/tmp}/accountable-release.XXXXXX")"
trap 'rm -rf "$work"' EXIT

aws s3api get-object --bucket "$bucket" \
	--key "releases/v1/manifests/$manifest_digest.json" "$work/release.json" >/dev/null
aws s3api get-object --bucket "$bucket" \
	--key "releases/v1/bundles/$bundle_digest.json" "$work/release.sigstore.json" >/dev/null

jq -n \
	--arg api "${image_uri##*@}" \
	--arg collector "${collector_image_uri##*@}" \
	--arg web "$(jq -r '.artifacts[] | select(.name == "web_assets") | .digest' "$work/release.json")" \
	'{api_oci: $api, collector_oci: $collector, web_assets: $web}' >"$work/artifacts.json"

go run ./cmd/release verify \
	--manifest "$work/release.json" \
	--bundle "$work/release.sigstore.json" \
	--manifest-digest "$manifest_digest" \
	--bundle-digest "$bundle_digest" \
	--artifacts "$work/artifacts.json" \
	--workflow-identity "$workflow_identity"

cosign verify \
	--certificate-identity "$api_image_identity" \
	--certificate-oidc-issuer https://token.actions.githubusercontent.com \
	"$image_uri" >/dev/null
cosign verify \
	--certificate-identity "$collector_image_identity" \
	--certificate-oidc-issuer https://token.actions.githubusercontent.com \
	"$collector_image_uri" >/dev/null

jq -c '{release_id, web_asset_digest: (.artifacts[] | select(.name == "web_assets") | .digest)}' "$work/release.json"
