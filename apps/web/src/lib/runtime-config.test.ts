import { describe, expect, it, vi } from "vitest";
import { loadRuntimeConfig, parseRuntimeConfig } from "./runtime-config";

const valid = {
	schema_version: 1 as const,
	api_base_url: "http://127.0.0.1:8080",
	architecture_probe: false,
	configuration_revision: "local-test",
};

describe("runtime config", () => {
	it("accepts the strict public schema", () => {
		expect(parseRuntimeConfig(valid)).toEqual(valid);
	});

	it.each([
		["https://api.example.com/", "https://api.example.com"],
		["https://api.example.com/v1/", "https://api.example.com/v1"],
	])("normalises the trailing slash on %s", (input, expected) => {
		expect(parseRuntimeConfig({ ...valid, api_base_url: input })).toEqual({
			...valid,
			api_base_url: expected,
		});
	});

	it.each([
		{ ...valid, secret: "must-not-pass" },
		{ ...valid, schema_version: 2 },
		{ ...valid, api_base_url: "javascript:alert(1)" },
		{ ...valid, api_base_url: "https://token@example.com" },
		{ ...valid, api_base_url: "https://api.example.com/?token=1" },
		{ ...valid, api_base_url: "https://api.example.com/#token" },
		{ ...valid, configuration_revision: "not a revision" },
		{ ...valid, configuration_revision: "" },
	])("rejects invalid or expanded config: $api_base_url", (candidate) => {
		expect(() => parseRuntimeConfig(candidate)).toThrow();
	});

	it.each([[["not", "an", "object"]], [null], ["a string"]])(
		"rejects a non-object document: %s",
		(candidate) => {
			expect(() => parseRuntimeConfig(candidate)).toThrow();
		},
	);

	it("fetches before client construction without allowing cache reuse", async () => {
		const fetcher = vi.fn<typeof fetch>().mockResolvedValue(
			new Response(JSON.stringify(valid), {
				status: 200,
				headers: { "Content-Type": "application/json" },
			}),
		);
		await expect(loadRuntimeConfig(fetcher)).resolves.toEqual(valid);
		expect(fetcher).toHaveBeenCalledWith("/_runtime/config.json", {
			cache: "no-store",
			credentials: "same-origin",
			headers: { Accept: "application/json" },
		});
	});
});
