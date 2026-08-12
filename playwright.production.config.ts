import { defineConfig, devices } from "@playwright/test";

const baseURL = process.env.ACCOUNTABLE_CELL_URL;
if (!baseURL || !baseURL.startsWith("https://")) {
	throw new Error("ACCOUNTABLE_CELL_URL must be the HTTPS CloudFront cell URL");
}

export default defineConfig({
	testDir: "tests/integration",
	testMatch: ["web-edge-contract.spec.ts", "production-smoke.spec.ts"],
	fullyParallel: false,
	workers: 1,
	forbidOnly: true,
	retries: 0,
	reporter: process.env.CI ? "github" : "list",
	expect: { timeout: 20_000 },
	use: { baseURL, trace: "retain-on-failure" },
	projects: [
		{
			name: "chromium",
			use: { ...devices["Desktop Chrome"] },
		},
	],
});
