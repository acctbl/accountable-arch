#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

work="$(mktemp -d "${TMPDIR:-/tmp}/accountable-collector-validate.XXXXXX")"
trap 'rm -rf "$work"' EXIT

BUILDER="$(command -v builder || true)"
if [[ -z "$BUILDER" ]]; then
	echo "the pinned OpenTelemetry Collector builder is missing; run mise install --locked" >&2
	exit 1
fi
"$BUILDER" --skip-compilation --config docker/collector/builder-config.yaml
(cd dist/collector && go build -o "$work/accountable-collector" .)

# The generated golden configuration names the task-scoped scratch volume, which
# exists only inside a running task, so the proof reads an equivalent local file.
printf 'proof-token' >"$work/grafana-token"
sed "s|/run/accountable/grafana-token|$work/grafana-token|" \
	testdata/telemetry/collector/config/positive/collector.yaml >"$work/collector.yaml"

AWS_REGION="${AWS_REGION:-eu-west-2}" "$work/accountable-collector" validate --config "$work/collector.yaml"
echo "collector: the pinned distribution accepts the generated cell configuration"
