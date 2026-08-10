#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

node scripts/security/exceptions-to-allowlists.mjs
EXCLUDE_FILE=".security/allowlists/govulncheck-exclude.txt"

report="$(mktemp)"
trap 'rm -f "$report"' EXIT

govulncheck -format=json -scan=package ./cmd/... ./internal/... >"$report"

node --input-type=module - "$report" "$EXCLUDE_FILE" <<'EOF'
import { readFileSync } from "node:fs";

function* jsonObjects(text) {
	let i = 0;
	while (i < text.length) {
		while (i < text.length && /\s/.test(text[i])) i++;
		if (i >= text.length) return;
		const start = i;
		let depth = 0;
		let inString = false;
		let escape = false;
		for (; i < text.length; i++) {
			const c = text[i];
			if (inString) {
				if (escape) escape = false;
				else if (c === "\\") escape = true;
				else if (c === '"') inString = false;
				continue;
			}
			if (c === '"') inString = true;
			else if (c === "{") depth++;
			else if (c === "}") {
				depth--;
				if (depth === 0) {
					i++;
					yield JSON.parse(text.slice(start, i));
					break;
				}
			}
		}
	}
}

const [reportPath, excludePath] = process.argv.slice(2);
const exclude = new Set(
	readFileSync(excludePath, "utf8")
		.split("\n")
		.map((line) => line.trim())
		.filter((line) => line.startsWith("GO-")),
);

const findings = new Set();
for (const msg of jsonObjects(readFileSync(reportPath, "utf8"))) {
	const id = msg.finding?.osv;
	const reachesPackage = msg.finding?.trace?.some((frame) => frame.package);
	if (!id || !reachesPackage || exclude.has(id)) continue;
	findings.add(id);
}

if (findings.size === 0) {
	console.log("govulncheck: no actionable vulnerabilities");
	process.exit(0);
}

console.error(`govulncheck: ${findings.size} actionable vulnerabilit(y/ies):`);
for (const id of [...findings].sort()) {
	console.error(`  - ${id}`);
}
process.exit(1);
EOF
