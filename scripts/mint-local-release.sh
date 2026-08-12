#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
	echo "usage: mint-local-release.sh <absolute-output-path>" >&2
	exit 2
fi

output="$1"
if [[ "$output" != /* ]]; then
	echo "output path must be absolute" >&2
	exit 2
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

local_release_id="$(go run ./cmd/release work-id)"

go run ./cmd/release runtime-release \
	--release-id "$local_release_id" \
	--manifest-digest "sha256:$(printf 'local-manifest' | shasum -a 256 | cut -d" " -f1)" \
	--service-version "sha256:$(printf 'local-image' | shasum -a 256 | cut -d" " -f1)" \
	--web-digest "sha256:$(printf 'local-web' | shasum -a 256 | cut -d" " -f1)" \
	--output "$output"
