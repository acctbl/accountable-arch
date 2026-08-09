import * as v from "valibot";

const configurationRevisionPattern = /^[-A-Za-z0-9._]{1,128}$/;

function isSafeApiBaseUrl(value: string): boolean {
	let url: URL;
	try {
		url = new URL(value);
	} catch {
		return false;
	}
	return (
		(url.protocol === "http:" || url.protocol === "https:") &&
		url.username === "" &&
		url.password === "" &&
		url.search === "" &&
		url.hash === "" &&
		url.host !== ""
	);
}

const runtimeConfigSchema = v.strictObject({
	schema_version: v.literal(1),
	api_base_url: v.pipe(
		v.string(),
		v.check(isSafeApiBaseUrl, "Unsafe or invalid API base URL"),
		v.transform((value) => new URL(value).toString().replace(/\/$/, "")),
	),
	architecture_probe: v.boolean(),
	configuration_revision: v.pipe(
		v.string(),
		v.regex(configurationRevisionPattern),
	),
});

export type RuntimeConfig = v.InferOutput<typeof runtimeConfigSchema>;

export function parseRuntimeConfig(value: unknown): RuntimeConfig {
	return v.parse(runtimeConfigSchema, value);
}

export async function loadRuntimeConfig(
	fetcher: typeof fetch = fetch,
): Promise<RuntimeConfig> {
	const response = await fetcher("/_runtime/config.json", {
		cache: "no-store",
		credentials: "same-origin",
		headers: { Accept: "application/json" },
	});
	if (!response.ok) {
		throw new Error("Runtime configuration is unavailable");
	}
	return parseRuntimeConfig(await response.json());
}
