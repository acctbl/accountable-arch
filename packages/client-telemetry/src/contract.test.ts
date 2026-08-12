import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, test } from "vitest";

import {
	clientSampled,
	DocumentEmissionLimits,
	parseClientEnvelope,
	validateClientAdmission,
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

describe("shared parity vectors", () => {
	const readVectors = async () =>
		JSON.parse(
			await readFile(resolve(fixtureRoot, "parity/vectors.json"), "utf8"),
		) as {
			sampling: {
				correlation_id: string;
				event_name: string;
				sampled: boolean;
			}[];
			admission_window: {
				now: string;
				occurred_at: string;
				admitted: boolean;
			}[];
		};

	test("sampling matches the Go decision for every vector", async () => {
		const { sampling } = await readVectors();
		expect(sampling.length).toBeGreaterThan(0);
		for (const vector of sampling) {
			expect(
				await clientSampled(vector.correlation_id, vector.event_name),
			).toBe(vector.sampled);
		}
	});

	test("admission window matches the Go decision for every vector", async () => {
		const { admission_window } = await readVectors();
		const envelope = parseClientEnvelope(
			JSON.parse(
				await readFile(resolve(fixtureRoot, "positive/app-ready.json"), "utf8"),
			),
		);
		const activeIdentity = {
			client_application: envelope.client_application,
			release_id: envelope.release_id,
			client_artifact_type: envelope.client_artifact_type,
			client_artifact_digest: envelope.client_artifact_digest,
			configuration_schema_version: envelope.configuration_schema_version,
			configuration_revision: envelope.configuration_revision,
			configuration_fingerprint: envelope.configuration_fingerprint,
		};
		expect(admission_window.length).toBeGreaterThan(0);
		for (const vector of admission_window) {
			const attempt = () =>
				validateClientAdmission(
					{ ...envelope, occurred_at: vector.occurred_at },
					{ now: new Date(vector.now), activeIdentity },
				);
			if (vector.admitted) {
				expect(attempt).not.toThrow();
			} else {
				expect(attempt).toThrow();
			}
		}
	});
});

test("combined document limits share one counter", () => {
	const limits = new DocumentEmissionLimits();
	for (let index = 0; index < 199; index += 1) {
		expect(limits.accept("route.navigation")).toBe(true);
	}
	expect(limits.accept("api.request.completed")).toBe(true);
	expect(limits.accept("route.navigation")).toBe(false);
});
