import * as v from "valibot";

const configurationRevisionPattern = /^[-A-Za-z0-9._]{1,128}$/;

declare global {
	interface Window {
		__ACCOUNTABLE_RUNTIME_CONFIG__?: unknown;
	}
}

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

const webRuntimeReleaseSchema = v.strictObject({
	schema_version: v.literal(1),
	release_id: v.pipe(
		v.string(),
		v.regex(
			/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
		),
	),
	release_manifest_digest: v.pipe(v.string(), v.regex(/^sha256:[0-9a-f]{64}$/)),
	web_asset_digest: v.pipe(v.string(), v.regex(/^sha256:[0-9a-f]{64}$/)),
});

export type WebRuntimeRelease = v.InferOutput<typeof webRuntimeReleaseSchema>;

export function parseWebRuntimeRelease(value: unknown): WebRuntimeRelease {
	return v.parse(webRuntimeReleaseSchema, value);
}

export type RuntimeConfig = v.InferOutput<typeof runtimeConfigSchema>;

export type LoadedRuntimeConfig = RuntimeConfig & {
	configurationFingerprint: string;
};

export function parseRuntimeConfig(value: unknown): RuntimeConfig {
	return v.parse(runtimeConfigSchema, value);
}

export function peekRuntimeConfig(): RuntimeConfig | null {
	if (typeof window === "undefined") {
		return null;
	}

	const inline = window.__ACCOUNTABLE_RUNTIME_CONFIG__;
	if (inline === undefined) {
		return null;
	}

	try {
		return parseRuntimeConfig(inline);
	} catch {
		return null;
	}
}

export async function loadRuntimeConfig(
	fetcher: typeof fetch = fetch,
): Promise<LoadedRuntimeConfig> {
	const response = await fetcher("/_runtime/config.json", {
		cache: "no-store",
		credentials: "same-origin",
		headers: { Accept: "application/json" },
	});
	if (!response.ok) {
		throw new Error("Runtime configuration is unavailable");
	}
	const raw = new Uint8Array(await response.arrayBuffer());
	const text = new TextDecoder("utf-8", { fatal: true }).decode(raw);
	const configurationFingerprint = [
		...new Uint8Array(await crypto.subtle.digest("SHA-256", raw)),
	]
		.map((byte) => byte.toString(16).padStart(2, "0"))
		.join("");
	return {
		...parseRuntimeConfig(JSON.parse(text)),
		configurationFingerprint,
	};
}
