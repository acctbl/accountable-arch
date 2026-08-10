import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, test } from "vitest";

import {
	clientSampled,
	DocumentEmissionLimits,
	parseClientEnvelope,
} from "./index";

const fixtureRoot = resolve(
	fileURLToPath(new URL(".", import.meta.url)),
	"../../../testdata/telemetry/client",
);

describe("client contract fixtures", () => {
	test("accepts every positive fixture", async () => {
		for (const name of ["app-ready.json", "api-completed.json"]) {
			const value = JSON.parse(
				await readFile(resolve(fixtureRoot, "positive", name), "utf8"),
			);
			expect(() => parseClientEnvelope(value)).not.toThrow();
		}
	});

	test("rejects every negative fixture", async () => {
		for (const name of ["unknown-field.json", "mismatched-outcome.json"]) {
			const value = JSON.parse(
				await readFile(resolve(fixtureRoot, "negative", name), "utf8"),
			);
			expect(() => parseClientEnvelope(value)).toThrow();
		}
	});
});

test("sampling is deterministic", async () => {
	const first = await clientSampled(
		"018f47d2-7f00-7abc-8def-1234567890ab",
		"app.ready",
	);
	const second = await clientSampled(
		"018f47d2-7f00-7abc-8def-1234567890ab",
		"app.ready",
	);
	expect(first).toBe(second);
});

test("combined document limits share one counter", () => {
	const limits = new DocumentEmissionLimits();
	for (let index = 0; index < 199; index += 1) {
		expect(limits.accept("route.navigation")).toBe(true);
	}
	expect(limits.accept("api.request.completed")).toBe(true);
	expect(limits.accept("route.navigation")).toBe(false);
});
