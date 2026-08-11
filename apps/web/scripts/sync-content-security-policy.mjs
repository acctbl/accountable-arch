import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const webRoot = join(scriptDir, "..");
const policyPath = join(
	webRoot,
	"../../infra/opentofu/modules/cell/content-security-policy.txt",
);

const write = process.argv.includes("--write");

const vite = await createServer({
	configFile: join(webRoot, "vite.config.ts"),
	root: webRoot,
	server: { middlewareMode: true },
	appType: "custom",
});

try {
	const { contentSecurityPolicy } = await vite.ssrLoadModule("/edge-contract.ts");
	const nextPolicy = `${contentSecurityPolicy}\n`;
	const current = readFileSync(policyPath, "utf8");

	if (current === nextPolicy) {
		console.log(`${policyPath} is current`);
		process.exit(0);
	}

	if (!write) {
		console.error(
			`${policyPath} is out of date; run pnpm --filter @accountable/web csp:write`,
		);
		process.exit(1);
	}

	writeFileSync(policyPath, nextPolicy);
	console.log(`updated ${policyPath}`);
} finally {
	await vite.close();
}
