#!/usr/bin/env bash
set -euo pipefail

runbook_root="docs/runbooks/observability"
terraform_paths=(
	infra/opentofu/modules/cell/alarms.tf
	infra/opentofu/modules/grafana-stack/alerts.tf
	infra/opentofu/modules/observability-environment/main.tf
)

expected="$(mktemp "${TMPDIR:-/tmp}/accountable-runbooks-expected.XXXXXX")"
actual="$(mktemp "${TMPDIR:-/tmp}/accountable-runbooks-actual.XXXXXX")"
trap 'rm -f "$expected" "$actual"' EXIT

rg -o '\$\{var\.runbook_base_url\}/[a-z-]+' "${terraform_paths[@]}" |
	sed 's#.*\${var\.runbook_base_url}/##' |
	sort -u >"$expected"

find "$runbook_root" -mindepth 2 -maxdepth 2 -name README.md -print |
	sed "s#^$runbook_root/##; s#/README.md\$##" |
	sort -u >"$actual"

if ! diff -u "$expected" "$actual"; then
	echo "alert runbooks and repository runbooks differ" >&2
	exit 1
fi

while IFS= read -r runbook; do
	path="$runbook_root/$runbook/README.md"
	for heading in Diagnosis Mitigation Recovery Escalation; do
		rg -F "## $heading" "$path" >/dev/null
	done
done <"$expected"
