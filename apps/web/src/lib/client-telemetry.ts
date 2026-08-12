import {
	type ClientEnvelope,
	clientSampled,
	DocumentEmissionLimits,
	newCorrelationId,
	parseClientEnvelope,
} from "@accountable/client-telemetry";
import { type Metric, onCLS, onINP, onLCP } from "web-vitals";
import {
	type LoadedRuntimeConfig,
	parseWebRuntimeRelease,
	type WebRuntimeRelease,
} from "./runtime-config";

const correlationStorageKey = "accountable.correlation_id";
const limits = new DocumentEmissionLimits();
type RuntimeIdentity = {
	config: LoadedRuntimeConfig;
	release: WebRuntimeRelease;
};

let identity: RuntimeIdentity | null = null;

export function journeyCorrelationId(): string {
	const stored = sessionStorage.getItem(correlationStorageKey);
	if (stored) return stored;
	const created = newCorrelationId();
	sessionStorage.setItem(correlationStorageKey, created);
	return created;
}

export async function initializeClientTelemetry(
	config: LoadedRuntimeConfig,
): Promise<void> {
	if (new URL(config.api_base_url).origin !== location.origin) return;
	try {
		const response = await fetch("/_runtime/release.json", {
			cache: "no-store",
			credentials: "same-origin",
			headers: { Accept: "application/json" },
		});
		if (!response.ok) return;
		const raw = new Uint8Array(await response.arrayBuffer());
		const release = parseWebRuntimeRelease(
			JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(raw)),
		);
		identity = { config, release };
	} catch {
		return;
	}
	installRuntimeErrorReporting();
	installWebVitals();
	await emit("app.ready", {
		duration_ms: boundedMilliseconds(performance.now()),
	});
}

export async function recordAPIRequest(
	statusClass: "2xx" | "4xx" | "5xx" | "network",
	outcome: "success" | "failure",
	errorCategory:
		| "client"
		| "network"
		| "none"
		| "server"
		| "timeout"
		| "unknown",
	started: number,
): Promise<void> {
	await emit("api.request.completed", {
		rpc_service: "accountable.system.v1.SystemService",
		rpc_method: "GetRuntime",
		status_class: statusClass,
		outcome,
		error_category: errorCategory,
		duration_ms: boundedMilliseconds(performance.now() - started),
	});
	if (outcome === "failure") {
		await emit("api.request.failed", {
			rpc_service: "accountable.system.v1.SystemService",
			rpc_method: "GetRuntime",
			error_category:
				errorCategory === "client" || errorCategory === "none"
					? "unknown"
					: errorCategory,
		});
	}
}

async function emit(
	eventName: string,
	attributes: Record<string, string | number | boolean>,
): Promise<void> {
	const current = identity;
	if (!current) return;
	const correlationId = journeyCorrelationId();
	if (
		!(await clientSampled(correlationId, eventName)) ||
		!limits.accept(eventName)
	)
		return;
	const envelope = parseClientEnvelope({
		schema_version: 1,
		client_application: "web",
		event_name: eventName,
		occurred_at: new Date().toISOString(),
		correlation_id: correlationId,
		route_class: "home",
		release_id: current.release.release_id,
		client_artifact_type: "web_assets",
		client_artifact_digest: current.release.web_asset_digest,
		configuration_schema_version: current.config.schema_version,
		configuration_revision: current.config.configuration_revision,
		configuration_fingerprint: current.config.configurationFingerprint,
		attributes,
	} satisfies ClientEnvelope);
	void fetch(`${current.config.api_base_url}/telemetry/v1`, {
		method: "POST",
		body: JSON.stringify(envelope),
		credentials: "omit",
		keepalive: true,
		headers: {
			"Content-Type": "application/json",
			"X-Correlation-ID": correlationId,
		},
	}).catch(() => undefined);
}

function installRuntimeErrorReporting(): void {
	window.addEventListener(
		"error",
		(event) => {
			const category =
				event.target instanceof HTMLScriptElement
					? "script_error"
					: event.target instanceof HTMLElement
						? "resource_error"
						: "unknown";
			void emit("app.error", { error_category: category });
		},
		true,
	);
	window.addEventListener("unhandledrejection", () => {
		void emit("app.error", { error_category: "promise_rejection" });
	});
}

function installWebVitals(): void {
	onLCP((metric) => recordVital("web_vital.lcp", metric));
	onINP((metric) => recordVital("web_vital.inp", metric));
	onCLS((metric) => recordVital("web_vital.cls", metric));
}

function recordVital(eventName: string, metric: Metric): void {
	void emit(eventName, { value: metric.value });
}

function boundedMilliseconds(value: number): number {
	return Math.min(600_000, Math.max(0, Math.round(value)));
}
