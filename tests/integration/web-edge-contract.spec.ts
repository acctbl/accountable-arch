import { expect, test } from "@playwright/test";

import {
	cacheControl,
	contentSecurityPolicy,
	documentBootstrapScriptHash,
	documentContentSecurityPolicy,
} from "../../apps/web/edge-contract";
import { parseRuntimeConfig } from "../../apps/web/src/lib/runtime-config";

test("serves the runtime config artifact uncached with the strict schema", async ({
	request,
}) => {
	const response = await request.get("/_runtime/config.json");
	expect(response.status()).toBe(200);
	expect(response.headers()["cache-control"]).toBe(cacheControl.runtimeConfig);
	expect(response.headers()["content-type"]).toContain("application/json");

	expect(parseRuntimeConfig(await response.json())).toMatchObject({
		schema_version: 1,
	});
});

test("serves documents and deep links as revalidated SPA fallbacks", async ({
	request,
}) => {
	const root = await request.get("/");
	expect(root.status()).toBe(200);
	expect(root.headers()["cache-control"]).toBe(cacheControl.document);

	const deepLink = await request.get("/a/deep/route");
	expect(deepLink.status()).toBe(200);
	expect(deepLink.headers()["content-type"]).toContain("text/html");
	expect(deepLink.headers()["cache-control"]).toBe(cacheControl.document);
});

test("serves hashed assets as immutable", async ({ request }) => {
	const root = await request.get("/");
	const assetPath = (await root.text()).match(/\/assets\/[^"]+\.js/)?.[0];
	if (!assetPath) {
		throw new Error("index.html references no hashed asset");
	}

	const asset = await request.get(assetPath);
	expect(asset.status()).toBe(200);
	expect(asset.headers()["cache-control"]).toBe(cacheControl.hashedAsset);
});

test("serves documents with the edge content security policy", async ({
	request,
}) => {
	const runtimeConfig = parseRuntimeConfig(
		await (await request.get("/_runtime/config.json")).json(),
	);
	const document = await request.get("/");
	const expected = documentContentSecurityPolicy(
		contentSecurityPolicy,
		runtimeConfig.api_base_url,
		new URL(document.url()).origin,
	);

	expect(document.status()).toBe(200);
	expect(document.headers()["content-security-policy"]).toBe(expected);
});

test("pins the script that vite injects into the document", async ({
	request,
}) => {
	const digest = documentBootstrapScriptHash(false);

	const document = await request.get("/");
	const scriptSource = (document.headers()["content-security-policy"] ?? "")
		.split("; ")
		.find((directive) => directive.startsWith("script-src "));

	expect(scriptSource).toBe(`script-src 'self' 'sha256-${digest}'`);
	expect(contentSecurityPolicy).toContain(`'sha256-${digest}'`);
});
