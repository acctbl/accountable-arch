import { check, fail } from "k6";
import crypto from "k6/crypto";
import encoding from "k6/encoding";
import exec from "k6/execution";
import http from "k6/http";

const baseURL = required("ACCOUNTABLE_URL").replace(/\/$/, "");
const profile = required("ACCOUNTABLE_PROFILE");
const duration = required("ACCOUNTABLE_DURATION");
const rate = positiveInteger(required("ACCOUNTABLE_RATE"));
const proofKey = required("ACCOUNTABLE_PROOF_KEY");
const supportedProfiles = ["smoke", "baseline", "capacity", "soak", "controlled-failure"];

if (!baseURL.startsWith("https://") || !supportedProfiles.includes(profile)) {
	fail("the load-test contract is invalid");
}

export const options = {
	discardResponseBodies: false,
	scenarios: {
		workload: {
			executor: "constant-arrival-rate",
			rate,
			timeUnit: "1s",
			duration,
			preAllocatedVUs: Math.max(10, rate * 2),
			gracefulStop: "30s",
		},
	},
	thresholds: {
		checks: ["rate>0.99"],
		http_req_duration: ["p(95)<1000", "p(99)<2500"],
		http_req_failed: [profile === "controlled-failure" ? "rate<0.20" : "rate<0.01"],
	},
};

export function setup() {
	const configResponse = http.get(`${baseURL}/_runtime/config.json`, { tags: { request_class: "setup" } });
	const releaseResponse = http.get(`${baseURL}/_runtime/release.json`, { tags: { request_class: "setup" } });
	if (configResponse.status !== 200 || releaseResponse.status !== 200) {
		fail("runtime identity is unavailable");
	}
	const config = configResponse.json();
	const release = releaseResponse.json();
	return {
		api: config.api_base_url,
		revision: config.configuration_revision,
		fingerprint: crypto.sha256(configResponse.body, "hex"),
		releaseId: release.release_id,
		webDigest: release.web_asset_digest,
	};
}

export default function (identity) {
	const correlation = sampledCorrelation("api.request.completed", (exec.vu.idInTest * 131 + exec.scenario.iterationInTest) % 128);
	const headers = {
		"Connect-Protocol-Version": "1",
		"Content-Type": "application/json",
		"X-Correlation-ID": correlation,
		"X-Privacy-Probe": "email@example.invalid bearer-secret session-token",
	};
	const started = Date.now();
	const response = http.post(`${identity.api}/accountable.system.v1.SystemService/GetRuntime`, "{}", {
		headers,
		tags: { request_class: "eligible" },
	});
	check(response, { "eligible request succeeded": (value) => value.status === 200 });
	http.get(`${baseURL}/api/_health/live`, {
		headers: { "X-Correlation-ID": correlation },
		tags: { request_class: "excluded" },
	});
	emitClient(identity, correlation, "api.request.completed", {
		rpc_service: "accountable.system.v1.SystemService",
		rpc_method: "GetRuntime",
		status_class: response.status >= 500 ? "5xx" : response.status >= 400 ? "4xx" : "2xx",
		outcome: response.status >= 500 ? "failure" : "success",
		error_category: response.status >= 500 ? "server" : "none",
		duration_ms: Math.min(600000, Date.now() - started),
	});
	if (exec.scenario.iterationInTest % 20 === 0) {
		emitClient(identity, correlation, "web_vital.lcp", { value: 1200 });
		emitClient(identity, correlation, "web_vital.inp", { value: 90 });
		emitClient(identity, correlation, "web_vital.cls", { value: 0.05 });
	}
	if (profile === "controlled-failure" && exec.scenario.iterationInTest % 10 === 0) {
		const failed = http.post(`${identity.api}/accountable.probe.v1.ArchitectureProbeService/Fail`, JSON.stringify({ kind: "unavailable" }), {
			headers,
			tags: { request_class: "controlled_failure" },
		});
		check(failed, { "controlled failure returned Connect error": (value) => value.status === 200 });
		emitClient(identity, correlation, "api.request.failed", {
			rpc_service: "accountable.system.v1.SystemService",
			rpc_method: "GetRuntime",
			error_category: "server",
		});
	}
	if (exec.vu.idInTest === 1 && exec.scenario.iterationInTest === 0) {
		emitProof(identity.api);
	}
}

function emitClient(identity, correlation, eventName, attributes) {
	const admittedCorrelation = eventName === "api.request.completed" || eventName === "api.request.failed"
		? correlation
		: sampledCorrelation(eventName, (exec.vu.idInTest * 131 + exec.scenario.iterationInTest) % 128);
	const envelope = JSON.stringify({
		schema_version: 1,
		client_application: "web",
		event_name: eventName,
		occurred_at: new Date().toISOString(),
		correlation_id: admittedCorrelation,
		route_class: "home",
		release_id: identity.releaseId,
		client_artifact_type: "web_assets",
		client_artifact_digest: identity.webDigest,
		configuration_schema_version: 1,
		configuration_revision: identity.revision,
		configuration_fingerprint: identity.fingerprint,
		attributes,
	});
	const response = http.post(`${identity.api}/telemetry/v1`, envelope, {
		headers: { "Content-Type": "application/json", Origin: baseURL, "X-Correlation-ID": admittedCorrelation },
		tags: { request_class: "browser_telemetry" },
	});
	check(response, { "browser telemetry was admitted": (value) => value.status === 202 });
}

function emitProof(apiBaseURL) {
	const body = '{"schema_version":1,"event_name":"observability.proof"}';
	const timestamp = `${Math.floor(Date.now() / 1000)}`;
	const nonce = uuidV7(`proof-${Date.now()}`);
	const canonical = `POST\n/telemetry/v1\n${timestamp}\n${nonce}\n${crypto.sha256(body, "hex")}`;
	const signature = crypto.hmac("sha256", encoding.b64decode(proofKey, "rawurl"), canonical, "hex");
	const response = http.post(`${apiBaseURL}/telemetry/v1`, body, {
		headers: {
			"Content-Type": "application/json",
			"X-Accountable-Proof-Timestamp": timestamp,
			"X-Accountable-Proof-Nonce": nonce,
			"X-Accountable-Proof-Signature": signature,
		},
		tags: { request_class: "authenticated_proof" },
	});
	check(response, { "authenticated proof was admitted": (value) => value.status === 202 });
}

function sampledCorrelation(eventName, index) {
	for (let candidate = 0; candidate < 10000; candidate += 1) {
		const value = uuidV7(`correlation-${index}-${candidate}`, 1_720_000_000_000);
		const prefix = crypto.sha256(`${value}\n${eventName}`, "hex").slice(0, 8);
		const basisPoints = eventName === "app.error" || eventName === "api.request.failed" ? 10000 : 1000;
		if (Number.parseInt(prefix, 16) % 10000 < basisPoints) return value;
	}
	fail("a bounded sampled correlation identity could not be produced");
}

function uuidV7(seed, instant = Date.now()) {
	const milliseconds = instant.toString(16).padStart(12, "0").slice(-12);
	const random = crypto.sha256(seed, "hex");
	const value = `${milliseconds}${random.slice(0, 20)}`;
	return `${value.slice(0, 8)}-${value.slice(8, 12)}-7${value.slice(13, 16)}-8${value.slice(17, 20)}-${value.slice(20, 32)}`;
}

function required(name) {
	const value = __ENV[name];
	if (!value) fail(`${name} is required`);
	return value;
}

function positiveInteger(value) {
	const parsed = Number.parseInt(value, 10);
	if (!Number.isSafeInteger(parsed) || parsed < 1 || `${parsed}` !== value) fail("ACCOUNTABLE_RATE must be a positive integer");
	return parsed;
}
