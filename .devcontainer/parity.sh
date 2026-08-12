#!/usr/bin/env bash
set -euo pipefail

mise trust mise.toml
mise install --locked
if [[ -S /var/run/docker.sock ]]; then
	export ACCOUNTABLE_TEST_POSTGRES_NETWORK_CONTAINER="$(hostname)"
fi
exec mise exec -- task ci
