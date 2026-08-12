#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

ENVIRONMENT="${1:?usage: build-lambdas.sh <environment>}"
if (( $# != 1 )); then
	echo "usage: build-lambdas.sh <environment>" >&2
	exit 2
fi
DOCUMENT_DIR="infra/observability/$ENVIRONMENT"
OUTPUT_DIR="dist/lambda/$ENVIRONMENT"

if [[ ! -d "$DOCUMENT_DIR" ]]; then
	echo "no observability documents for environment $ENVIRONMENT" >&2
	exit 1
fi

rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

package() {
	local name="$1" command="$2"
	local document="$DOCUMENT_DIR/$name.json"
	local staging="$OUTPUT_DIR/$name"

	if [[ ! -f "$document" ]]; then
		echo "missing deployment document: $document" >&2
		exit 1
	fi
	mkdir -p "$staging"
	cp "$document" "$staging/deployment.json"
	if grep -qE 'REPLACE_WITH_|REQUIRED_' "$staging/deployment.json"; then
		echo "deployment document $document still carries an unresolved value" >&2
		grep -nE 'REPLACE_WITH_|REQUIRED_' "$staging/deployment.json" >&2
		exit 1
	fi
	if ! jq empty "$staging/deployment.json" 2>/dev/null; then
		echo "deployment document $document is not valid JSON" >&2
		exit 1
	fi

	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
		-trimpath -ldflags="-s -w -buildid=" -o "$staging/bootstrap" "$command"
	touch -t 198001010000 "$staging/bootstrap" "$staging/deployment.json"
	(cd "$staging" && zip -q -X "../$name.zip" bootstrap deployment.json)
	rm -rf "$staging"
	echo "built $OUTPUT_DIR/$name.zip"
}

package translator ./cmd/alert-translator
package canary ./cmd/grafana-canary
if [[ "$ENVIRONMENT" == "development" ]]; then
	package capacity ./cmd/grafana-canary
fi
