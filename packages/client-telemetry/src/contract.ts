import {
	clientRegistry,
	type ClientEnvelope,
	parseClientEnvelopeSchema,
} from "./generated/client-contract.schema";

export type ClientIdentityTuple = Pick<
	ClientEnvelope,
	| "client_application"
	| "release_id"
	| "client_artifact_type"
	| "client_artifact_digest"
	| "configuration_schema_version"
	| "configuration_revision"
	| "configuration_fingerprint"
>;

export type ClientAdmissionPolicy = {
	now: Date;
	activeIdentity: ClientIdentityTuple;
	previousIdentity?: ClientIdentityTuple;
};

type AttributeRule = {
	type: "integer" | "number" | "string";
	minimum?: number;
	maximum?: number;
	values?: readonly string[];
};

type EventDefinition = {
	name: string;
	sample_basis_points: number;
	emission_group: string;
	document_limit: number;
	attributes: Readonly<Record<string, AttributeRule>>;
	required_attributes: readonly string[];
};

const eventRegistry: ReadonlyMap<string, EventDefinition> = new Map(
	clientRegistry.events.map((event) => [event.name, event as EventDefinition]),
);

export function parseClientEnvelope(input: unknown): ClientEnvelope {
	const envelope = parseClientEnvelopeSchema(input);
	const occurredAt = new Date(envelope.occurred_at);
	if (
		Number.isNaN(occurredAt.getTime()) ||
		occurredAt.toISOString() !== envelope.occurred_at
	) {
		throw new Error("invalid occurred_at");
	}
	validateRegisteredEnvelope(envelope);
	return envelope;
}

export function validateClientAdmission(
	envelope: ClientEnvelope,
	policy: ClientAdmissionPolicy,
): void {
	const occurredAt = Date.parse(envelope.occurred_at);
	const now = policy.now.getTime();
	if (occurredAt < now - 5 * 60_000 || occurredAt > now + 60_000) {
		throw new Error("invalid occurred_at");
	}
	const identity = clientIdentity(envelope);
	if (
		!sameIdentity(identity, policy.activeIdentity) &&
		(!policy.previousIdentity ||
			!sameIdentity(identity, policy.previousIdentity))
	) {
		throw new Error("invalid client identity tuple");
	}
}

export async function clientSampled(
	correlationId: string,
	eventName: string,
): Promise<boolean> {
	const event = eventRegistry.get(eventName);
	if (!event) throw new Error("invalid event_name");
	const input = new TextEncoder().encode(`${correlationId}\n${eventName}`);
	const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", input));
	const bucket = new DataView(
		digest.buffer,
		digest.byteOffset,
		digest.byteLength,
	).getUint32(0, false);
	return bucket % 10_000 < event.sample_basis_points;
}

export class DocumentEmissionLimits {
	readonly #counts = new Map<string, number>();

	accept(eventName: string): boolean {
		const event = eventRegistry.get(eventName);
		if (!event) return false;
		const count = this.#counts.get(event.emission_group) ?? 0;
		if (count >= event.document_limit) return false;
		this.#counts.set(event.emission_group, count + 1);
		return true;
	}
}

function validateRegisteredEnvelope(envelope: ClientEnvelope): void {
	if (
		!includesString(
			clientRegistry.client_applications,
			envelope.client_application,
		)
	) {
		throw new Error("invalid client_application");
	}
	if (
		!includesString(
			clientRegistry.client_artifact_types,
			envelope.client_artifact_type,
		)
	) {
		throw new Error("invalid client_artifact_type");
	}
	if (!includesString(clientRegistry.route_classes, envelope.route_class)) {
		throw new Error("invalid route_class");
	}
	const event = eventRegistry.get(envelope.event_name);
	if (!event) throw new Error("invalid event_name");
	const attributeNames = Object.keys(envelope.attributes);
	if (attributeNames.length !== event.required_attributes.length) {
		throw new Error("invalid attributes");
	}
	for (const name of event.required_attributes) {
		const value = envelope.attributes[name];
		if (value === undefined) throw new Error("invalid attributes");
		const rule = event.attributes[name];
		if (!rule) throw new Error("invalid attributes");
		validateAttribute(value, rule);
	}
	if (event.name === "api.request.completed") {
		const statusClass = envelope.attributes["status_class"];
		const outcome = envelope.attributes["outcome"];
		if (
			(statusClass === "5xx" || statusClass === "network") !==
			(outcome === "failure")
		) {
			throw new Error("invalid outcome");
		}
	}
}

function includesString(values: readonly string[], candidate: string): boolean {
	return values.includes(candidate);
}

function validateAttribute(
	value: string | number | boolean,
	rule: AttributeRule,
): void {
	if (rule.type === "string") {
		if (typeof value !== "string" || !rule.values?.includes(value)) {
			throw new Error("invalid attribute");
		}
		return;
	}
	if (typeof value !== "number" || !Number.isFinite(value)) {
		throw new Error("invalid attribute");
	}
	if (rule.type === "integer" && !Number.isInteger(value)) {
		throw new Error("invalid attribute");
	}
	if (
		(rule.minimum !== undefined && value < rule.minimum) ||
		(rule.maximum !== undefined && value > rule.maximum)
	) {
		throw new Error("invalid attribute");
	}
}

function clientIdentity(envelope: ClientEnvelope): ClientIdentityTuple {
	return {
		client_application: envelope.client_application,
		release_id: envelope.release_id,
		client_artifact_type: envelope.client_artifact_type,
		client_artifact_digest: envelope.client_artifact_digest,
		configuration_schema_version: envelope.configuration_schema_version,
		configuration_revision: envelope.configuration_revision,
		configuration_fingerprint: envelope.configuration_fingerprint,
	};
}

function sameIdentity(
	left: ClientIdentityTuple,
	right: ClientIdentityTuple,
): boolean {
	return Object.keys(left).every(
		(key) =>
			left[key as keyof ClientIdentityTuple] ===
			right[key as keyof ClientIdentityTuple],
	);
}
