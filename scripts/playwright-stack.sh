#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
: "${ACCOUNTABLE_TEST_POSTGRES_HOST:?run through scripts/with-test-postgres.sh}"
: "${ACCOUNTABLE_TEST_POSTGRES_PORT:?run through scripts/with-test-postgres.sh}"
: "${ACCOUNTABLE_TEST_POSTGRES_DATABASE:?run through scripts/with-test-postgres.sh}"
: "${ACCOUNTABLE_TEST_POSTGRES_USER:?run through scripts/with-test-postgres.sh}"
: "${ACCOUNTABLE_TEST_POSTGRES_PASSWORD:?run through scripts/with-test-postgres.sh}"

STACK_TLS_DIR="$(mktemp -d)"
openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 1 \
	-subj "/CN=localhost" \
	-addext "subjectAltName=DNS:localhost,IP:127.0.0.1" \
	-keyout "$STACK_TLS_DIR/key.pem" \
	-out "$STACK_TLS_DIR/cert.pem" >/dev/null 2>&1

pnpm --filter @accountable/web build

API_CONFIG="$STACK_TLS_DIR/api.toml"
MIGRATE_CONFIG="$STACK_TLS_DIR/migrate.toml"
API_RELEASE="$STACK_TLS_DIR/release.json"
SECRETS_DIR="$STACK_TLS_DIR/secrets"
STORAGE_DIR="$STACK_TLS_DIR/storage"
mkdir -p "$SECRETS_DIR" "$STORAGE_DIR"
printf '%s' "$ACCOUNTABLE_TEST_POSTGRES_PASSWORD" >"$SECRETS_DIR/database.password"
openssl rand -base64 32 >"$SECRETS_DIR/crypto.primary_key"
chmod 0600 "$SECRETS_DIR/database.password" "$SECRETS_DIR/crypto.primary_key"
write_foundation() {
	local role="$1"
	cat <<EOF
schema_version = 1
revision = "playwright"
environment = "development"
deployment_mode = "local"
cell_id = "local"
aws_region = "eu-west-2"
runtime_role = "$role"
foundation_check_timeout = "30s"
readiness_probe_interval = "250ms"

[capabilities]
architecture_probe = $([ "$role" = "api" ] && printf true || printf false)
postgres = true
secrets = true
kms = true
object_storage = true
telemetry = false
redpanda = false
EOF
}

{
	write_foundation api
	cat <<EOF

[client_telemetry]
release_id = "018f47d2-7f00-7abc-8def-1234567890ab"
web_asset_digest = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
configuration_schema_version = 1
configuration_revision = "playwright"
configuration_fingerprint = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
proof_key_active_ref = "proof-key-active"
proof_key_standby_ref = "proof-key-standby"

[server]
listen_address = "127.0.0.1:18080"
allowed_origins = ["https://127.0.0.1:4173"]
trusted_proxy_cidrs = ["127.0.0.1/32", "::1/128"]
tls_certificate_file = "$STACK_TLS_DIR/cert.pem"
tls_private_key_file = "$STACK_TLS_DIR/key.pem"
unary_rpc_timeout = "10s"
stream_rpc_timeout = "25s"
EOF
} >"$API_CONFIG"

write_foundation migrate >"$MIGRATE_CONFIG"

for config in "$API_CONFIG" "$MIGRATE_CONFIG"; do
	cat >>"$config" <<EOF

[secrets]
provider = "file"
directory = "$SECRETS_DIR"

[postgres]
host = "$ACCOUNTABLE_TEST_POSTGRES_HOST"
port = $ACCOUNTABLE_TEST_POSTGRES_PORT
name = "$ACCOUNTABLE_TEST_POSTGRES_DATABASE"
user = "$ACCOUNTABLE_TEST_POSTGRES_USER"
role = "$ACCOUNTABLE_TEST_POSTGRES_USER"
password_ref = "database.password"
tls_mode = "disable"
connect_timeout = "5s"
statement_timeout = "10s"
health_check_interval = "250ms"
max_connections = 16
timezone = "UTC"

[object_storage]
provider = "filesystem"
root = "$STORAGE_DIR"
access_purpose = "foundation-proof"

[kms]
provider = "local"
key_ref = "crypto.primary_key"
encryption_context_prefix = "accountable.foundation"

[time]
provider = "system"
max_clock_error = "1s"
max_database_skew = "5s"
EOF
done

bash scripts/mint-local-release.sh "$API_RELEASE"

mise exec -- go run ./cmd/migrate --config "$MIGRATE_CONFIG" --release "$API_RELEASE"
API_PID=""
WEB_PID=""
cleanup() {
	if [ -n "$WEB_PID" ]; then
		kill "$WEB_PID" 2>/dev/null || true
		wait "$WEB_PID" 2>/dev/null || true
	fi
	if [ -n "$API_PID" ]; then
		kill "$API_PID" 2>/dev/null || true
		wait "$API_PID" 2>/dev/null || true
	fi
	rm -f "$API_CONFIG" "$MIGRATE_CONFIG" "$API_RELEASE" "$STACK_TLS_DIR/cert.pem" "$STACK_TLS_DIR/key.pem"
	rm -f "$SECRETS_DIR/database.password" "$SECRETS_DIR/crypto.primary_key"
	rmdir "$SECRETS_DIR" "$STORAGE_DIR" 2>/dev/null || true
	rmdir "$STACK_TLS_DIR" 2>/dev/null || true
}
trap cleanup EXIT

mise exec -- go run ./cmd/api --config "$API_CONFIG" --release "$API_RELEASE" &
API_PID=$!

API_READY=false
for _ in $(seq 1 100); do
	if curl --fail --silent --show-error \
		--cacert "$STACK_TLS_DIR/cert.pem" \
		https://127.0.0.1:18080/_health/ready >/dev/null 2>&1; then
		API_READY=true
		break
	fi
	sleep 0.1
done
if [ "$API_READY" != true ]; then
	echo "API did not become ready" >&2
	exit 1
fi

export ACCOUNTABLE_RUNTIME_API_BASE_URL="https://127.0.0.1:18080"
export ACCOUNTABLE_RUNTIME_ARCHITECTURE_PROBE="true"
export ACCOUNTABLE_RUNTIME_CONFIGURATION_REVISION="playwright"
export ACCOUNTABLE_TLS_CERT_FILE="$STACK_TLS_DIR/cert.pem"
export ACCOUNTABLE_TLS_KEY_FILE="$STACK_TLS_DIR/key.pem"
export ACCOUNTABLE_WEB_HOST="127.0.0.1"
export ACCOUNTABLE_WEB_PORT="4173"
pnpm --filter @accountable/web exec vite preview --host 127.0.0.1 --port 4173 &
WEB_PID=$!

wait "$WEB_PID"
