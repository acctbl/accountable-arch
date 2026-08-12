import { expect, test, type Page } from "@playwright/test";

// Canonical Connect code numbers from the wire protocol. Keeping these in the
// browser test avoids giving the root Playwright package a second app runtime
// dependency solely for enum labels.
const connectCode = {
	canceled: 1,
	invalidArgument: 3,
	unavailable: 14,
} as const;

const uuidV7 =
	/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const expectedReleaseId = process.env.ACCOUNTABLE_EXPECTED_RELEASE_ID;

async function openProbeShell(page: Page) {
	await page.goto("/");
	await expect
		.poll(() =>
			page.evaluate(() => Boolean(window.__accountableArchitectureProbe)),
		)
		.toBe(true);
}

test("generated system client succeeds across the browser CORS boundary", async ({
	page,
}) => {
	const consoleErrors: string[] = [];
	page.on("console", (message) => {
		if (message.type() === "error") consoleErrors.push(message.text());
	});
	await openProbeShell(page);
	const runtime = await page.evaluate(() =>
		window.__accountableArchitectureProbe?.getRuntime(),
	);
	if (expectedReleaseId) {
		expect(runtime?.releaseId).toBe(expectedReleaseId);
	} else {
		expect(runtime?.releaseId).toMatch(uuidV7);
	}
	expect(runtime?.requestId).toMatch(uuidV7);
	expect(consoleErrors).toEqual([]);
});

test("decodes safe validation and unavailable ProblemDetail values", async ({
	page,
}) => {
	await openProbeShell(page);
	const invalid = await page.evaluate(() =>
		window.__accountableArchitectureProbe?.fail("invalid_input"),
	);
	expect(invalid).toMatchObject({
		code: connectCode.invalidArgument,
		category: "invalid_input",
		messageKey: "errors.invalidInput",
		fieldViolations: [{ fieldPath: "kind", code: "invalid_failure_kind" }],
	});
	expect(invalid?.problemId).toMatch(uuidV7);
	expect(invalid?.requestId).toMatch(uuidV7);
	expect(JSON.stringify(invalid)).not.toMatch(/sql|provider|stack|secret/i);

	const unavailable = await page.evaluate(() =>
		window.__accountableArchitectureProbe?.fail("unavailable"),
	);
	expect(unavailable).toMatchObject({
		code: connectCode.unavailable,
		category: "unavailable",
		messageKey: "errors.unavailable",
	});
});

test("cancels in-flight work at the API boundary", async ({ page }) => {
	await openProbeShell(page);
	const code = await page.evaluate(() =>
		window.__accountableArchitectureProbe?.cancel(),
	);
	expect(code).toBe(connectCode.canceled);
});

test("receives a Connect server stream incrementally", async ({ page }) => {
	await openProbeShell(page);
	const firstDelivery = await page.evaluate(() =>
		window.__accountableArchitectureProbe?.startStream(),
	);
	expect(firstDelivery).toEqual({ done: false, error: null, sequences: [1] });
	await expect
		.poll(() =>
			page.evaluate(
				() => window.__accountableArchitectureProbe?.streamState().done,
			),
		)
		.toBe(true);
	const state = await page.evaluate(() =>
		window.__accountableArchitectureProbe?.streamState(),
	);
	expect(state).toEqual({ done: true, error: null, sequences: [1, 2, 3] });
});

test("shares one browser/API correlation journey with distinct IDs", async ({
	page,
}) => {
	await openProbeShell(page);
	const correlation = await page.evaluate(() =>
		window.__accountableArchitectureProbe?.correlate(),
	);
	expect(correlation?.requestId).toMatch(uuidV7);
	expect(correlation?.traceId).toMatch(/^[0-9a-f-]{36}$/);
	expect(correlation?.requestId).not.toBe(correlation?.traceId);
});

test("round-trips the credentialed secure probe cookie", async ({ page }) => {
	await openProbeShell(page);
	await expect(
		page.evaluate(() =>
			window.__accountableArchitectureProbe?.cookieRoundTrip(),
		),
	).resolves.toBe(true);
});
