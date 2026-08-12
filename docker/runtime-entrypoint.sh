#!/bin/sh
set -eu

mode="${1:-}"
case "$mode" in
	api|migrate|preflight)
		: "${ACCOUNTABLE_CONFIG_BASE64:?ACCOUNTABLE_CONFIG_BASE64 is required}"
		: "${ACCOUNTABLE_RELEASE_BASE64:?ACCOUNTABLE_RELEASE_BASE64 is required}"
		umask 077
		printf '%s' "$ACCOUNTABLE_CONFIG_BASE64" | base64 -d > /run/accountable/config.toml
		printf '%s' "$ACCOUNTABLE_RELEASE_BASE64" | base64 -d > /run/accountable/release.json
		unset ACCOUNTABLE_CONFIG_BASE64 ACCOUNTABLE_RELEASE_BASE64
		exec "/usr/local/bin/accountable-$mode" \
			--config /run/accountable/config.toml \
			--release /run/accountable/release.json
		;;
	bootstrap)
		: "${ACCOUNTABLE_CONFIG_BASE64:?ACCOUNTABLE_CONFIG_BASE64 is required}"
		: "${ACCOUNTABLE_RELEASE_BASE64:?ACCOUNTABLE_RELEASE_BASE64 is required}"
		: "${ACCOUNTABLE_DATABASE_MASTER_PASSWORD:?ACCOUNTABLE_DATABASE_MASTER_PASSWORD is required}"
		umask 077
		mkdir -p /run/accountable/secrets
		printf '%s' "$ACCOUNTABLE_CONFIG_BASE64" | base64 -d > /run/accountable/config.toml
		printf '%s' "$ACCOUNTABLE_RELEASE_BASE64" | base64 -d > /run/accountable/release.json
		printf '%s' "$ACCOUNTABLE_DATABASE_MASTER_PASSWORD" > /run/accountable/secrets/database-master-password
		unset ACCOUNTABLE_CONFIG_BASE64 ACCOUNTABLE_RELEASE_BASE64 ACCOUNTABLE_DATABASE_MASTER_PASSWORD
		exec /usr/local/bin/accountable-bootstrap \
			--config /run/accountable/config.toml \
			--release /run/accountable/release.json
		;;
	collector-init)
		: "${ACCOUNTABLE_CONFIG_BASE64:?ACCOUNTABLE_CONFIG_BASE64 is required}"
		: "${ECS_CONTAINER_METADATA_URI_V4:?ECS_CONTAINER_METADATA_URI_V4 is required}"
		umask 077
		printf '%s' "$ACCOUNTABLE_CONFIG_BASE64" | base64 -d > /run/accountable/collector-init.json
		unset ACCOUNTABLE_CONFIG_BASE64
		exec /usr/local/bin/accountable-collector-init \
			--config /run/accountable/collector-init.json \
			--metadata-uri "$ECS_CONTAINER_METADATA_URI_V4"
		;;
	*)
		echo "runtime mode must be api, bootstrap, collector-init, migrate, or preflight" >&2
		exit 2
		;;
esac
