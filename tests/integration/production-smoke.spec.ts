import { expect, test } from "@playwright/test";

import { parseRuntimeConfig } from "../../apps/web/src/lib/runtime-config";

const expectedReleaseId = process.env.ACCOUNTABLE_EXPECTED_RELEASE_ID;
const productionTarget = process.env.ACCOUNTABLE_CELL_URL;

test("serves the approved release through the production route", async ({
	page,
	request,
}) => {
	test.skip(!productionTarget, "production smoke runs only against the production cell");
	if (!expectedReleaseId) {
		throw new Error("ACCOUNTABLE_EXPECTED_RELEASE_ID is required");
	}
	const errors: string[] = [];
	page.on("console", (message) => {
		if (message.type() === "error") errors.push(message.text());
	});

	const config = parseRuntimeConfig(
		await (await request.get("/_runtime/config.json")).json(),
	);
	const response = await request.post(
		`${config.api_base_url}/accountable.system.v1.SystemService/GetRuntime`,
		{
			data: {},
			headers: {
				"Connect-Protocol-Version": "1",
				"Content-Type": "application/json",
			},
		},
	);
	expect(response.status()).toBe(200);
	expect((await response.json()).releaseId).toBe(expectedReleaseId);

	await page.goto("/");
	await expect(page.getByText(expectedReleaseId, { exact: false })).toBeVisible();
	expect(errors).toEqual([]);
});
