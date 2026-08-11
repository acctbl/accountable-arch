#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -eq 0 ]; then
	echo "usage: with-test-postgres.sh <command> [args...]" >&2
	exit 2
fi

if ! command -v docker >/dev/null 2>&1; then
	echo "Docker is required for local Postgres." >&2
	echo "Install a container engine that provides the docker CLI, then re-run this command." >&2
	exit 1
fi

if ! docker info >/dev/null 2>&1; then
	echo "Docker is required for local Postgres, but the daemon is not running." >&2
	echo "Start Docker Desktop (or your container engine), then re-run this command." >&2
	exit 1
fi

: "${POSTGRES_TEST_IMAGE:?POSTGRES_TEST_IMAGE is unset. Run via mise: mise exec -- <command>}"

TEST_POSTGRES_PASSWORD="accountable-local-test-only"
TEST_POSTGRES_DATABASE="accountable"
POSTGRES_CONTAINER="accountable-postgres-test-$$"

cleanup() {
	docker rm --force "$POSTGRES_CONTAINER" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker run --detach --rm \
	--name "$POSTGRES_CONTAINER" \
	--env POSTGRES_PASSWORD="$TEST_POSTGRES_PASSWORD" \
	--env POSTGRES_DB="$TEST_POSTGRES_DATABASE" \
	--publish 127.0.0.1::5432 \
	"$POSTGRES_TEST_IMAGE" >/dev/null

POSTGRES_PORT="$(docker inspect --format '{{(index (index .NetworkSettings.Ports "5432/tcp") 0).HostPort}}' "$POSTGRES_CONTAINER")"
POSTGRES_READY="false"
for _ in $(seq 1 300); do
	if docker exec "$POSTGRES_CONTAINER" pg_isready --host 127.0.0.1 --username postgres --dbname "$TEST_POSTGRES_DATABASE" >/dev/null 2>&1; then
		POSTGRES_READY="true"
		break
	fi
	sleep 0.1
done
if [ "$POSTGRES_READY" != "true" ]; then
	echo "disposable Postgres did not become ready" >&2
	exit 1
fi

export ACCOUNTABLE_REQUIRE_POSTGRES=1
export ACCOUNTABLE_TEST_POSTGRES_DSN="postgres://postgres:${TEST_POSTGRES_PASSWORD}@127.0.0.1:${POSTGRES_PORT}/${TEST_POSTGRES_DATABASE}?sslmode=disable"
export ACCOUNTABLE_TEST_POSTGRES_HOST="127.0.0.1"
export ACCOUNTABLE_TEST_POSTGRES_PORT="$POSTGRES_PORT"
export ACCOUNTABLE_TEST_POSTGRES_DATABASE="$TEST_POSTGRES_DATABASE"
export ACCOUNTABLE_TEST_POSTGRES_USER="postgres"
export ACCOUNTABLE_TEST_POSTGRES_PASSWORD="$TEST_POSTGRES_PASSWORD"
"$@"
