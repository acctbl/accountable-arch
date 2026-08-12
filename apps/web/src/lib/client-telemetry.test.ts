import { afterEach, expect, test, vi } from "vitest";

vi.mock("web-vitals", () => ({
	onCLS: vi.fn(),
	onINP: vi.fn(),
	onLCP: vi.fn(),
}));

import { initializeClientTelemetry } from "./client-telemetry";

afterEach(() => {
	vi.unstubAllGlobals();
	sessionStorage.clear();
});

test("telemetry transport failure does not become an application error", async () => {
	sessionStorage.setItem(
		"accountable.correlation_id",
		"019261bb-4c10-7f3a-9aaa-0f1e2d3c4b5a",
	);
	const fetcher = vi
		.fn<typeof fetch>()
		.mockResolvedValueOnce(
			new Response(
				JSON.stringify({
					schema_version: 1,
					release_id: "019261bb-4c10-7f3a-9aaa-0f1e2d3c4b5a",
					release_manifest_digest: `sha256:${"a".repeat(64)}`,
					web_asset_digest: `sha256:${"b".repeat(64)}`,
				}),
				{ status: 200 },
			),
		)
		.mockRejectedValueOnce(new Error("transport unavailable"));
	vi.stubGlobal("fetch", fetcher);
	const unhandled = vi.fn();
	window.addEventListener("unhandledrejection", unhandled);

	await initializeClientTelemetry({
		schema_version: 1,
		api_base_url: location.origin,
		architecture_probe: false,
		configuration_revision: "test",
		configurationFingerprint: "c".repeat(64),
	});
	await new Promise<void>((resolve) => {
		setTimeout(resolve, 0);
	});

	expect(fetcher).toHaveBeenCalledTimes(2);
	expect(unhandled).not.toHaveBeenCalled();
	window.removeEventListener("unhandledrejection", unhandled);
});
