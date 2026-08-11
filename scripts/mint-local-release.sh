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

local_release_id="$(python3 -c '
import os, time, uuid
raw = bytearray(os.urandom(16))
raw[0:6] = int(time.time() * 1000).to_bytes(6, "big")
raw[6] = (raw[6] & 0x0F) | 0x70
raw[8] = (raw[8] & 0x3F) | 0x80
print(str(uuid.UUID(bytes=bytes(raw))))
')"

go run ./cmd/release runtime-release \
	--release-id "$local_release_id" \
	--manifest-digest "sha256:$(printf 'local-manifest' | shasum -a 256 | cut -d" " -f1)" \
	--service-version "sha256:$(printf 'local-image' | shasum -a 256 | cut -d" " -f1)" \
	--web-digest "sha256:$(printf 'local-web' | shasum -a 256 | cut -d" " -f1)" \
	--output "$output"
