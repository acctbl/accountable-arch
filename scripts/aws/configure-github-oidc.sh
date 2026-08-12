#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" != "--trust-ready" || $# -ne 1 ]]; then
	echo "usage: configure-github-oidc.sh --trust-ready" >&2
	echo "apply the exact AWS trust policies in all three accounts before running this command" >&2
	exit 2
fi

repository="acctbl/accountable"
endpoint="repos/$repository/actions/oidc/customization/sub"
payload="$(mktemp "${TMPDIR:-/tmp}/accountable-github-oidc.XXXXXX")"
trap 'rm -f "$payload"' EXIT

jq -n '{use_default:false,include_claim_keys:["repo","context","workflow_ref"],use_immutable_subject:true}' >"$payload"
gh api --method PUT \
	-H "Accept: application/vnd.github+json" \
	-H "X-GitHub-Api-Version: 2026-03-10" \
	"$endpoint" \
	--input "$payload" >/dev/null

gh api \
	-H "Accept: application/vnd.github+json" \
	-H "X-GitHub-Api-Version: 2026-03-10" \
	"$endpoint" \
	| jq -e '.use_default == false and .include_claim_keys == ["repo","context","workflow_ref"]' >/dev/null

previous_run="$(gh run list --repo "$repository" --workflow oidc-contract.yml --event workflow_dispatch --branch main --limit 1 --json databaseId --jq '.[0].databaseId // 0')"
gh workflow run oidc-contract.yml --repo "$repository" --ref main

run_id=""
for _ in $(seq 1 30); do
	candidate="$(gh run list --repo "$repository" --workflow oidc-contract.yml --event workflow_dispatch --branch main --limit 1 --json databaseId --jq '.[0].databaseId // 0')"
	if [[ "$candidate" != "0" && "$candidate" != "$previous_run" ]]; then
		run_id="$candidate"
		break
	fi
	sleep 2
done

if [[ -z "$run_id" ]]; then
	echo "the OIDC contract run did not start" >&2
	exit 1
fi

gh run watch "$run_id" --repo "$repository" --exit-status
