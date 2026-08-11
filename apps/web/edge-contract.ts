import { createHash } from "node:crypto";
import { documentBootstrapScript } from "./src/document-bootstrap";

export const cacheControl = {
	runtimeConfig: "no-store",
	hashedAsset: "public, max-age=31536000, immutable",
	document: "no-cache",
} as const;

const CONTENT_SECURITY_POLICY_TEMPLATE = [
	"default-src 'self'",
	"script-src 'self' 'sha256-SCRIPT_HASH'",
	"style-src 'self' 'unsafe-inline'",
	"img-src 'self' data:",
	"font-src 'self'",
	"connect-src 'self'",
	"worker-src 'self'",
	"manifest-src 'self'",
	"base-uri 'none'",
	"object-src 'none'",
	"frame-ancestors 'none'",
	"form-action 'self'",
].join("; ");

export function documentBootstrapScriptHash(
	includePseudoLocales = false,
): string {
	return createHash("sha256")
		.update(documentBootstrapScript(includePseudoLocales))
		.digest("base64");
}

export function buildContentSecurityPolicy(
	includePseudoLocales = false,
): string {
	return CONTENT_SECURITY_POLICY_TEMPLATE.replace(
		"SCRIPT_HASH",
		documentBootstrapScriptHash(includePseudoLocales),
	);
}

export const contentSecurityPolicy = buildContentSecurityPolicy(false);

export function withConnectSource(policy: string, source: string): string {
	return policy
		.split("; ")
		.map((directive) =>
			directive.startsWith("connect-src ")
				? `${directive} ${source}`
				: directive,
		)
		.join("; ");
}

export function documentContentSecurityPolicy(
	policy: string,
	apiBaseURL: string | undefined,
	documentOrigin: string,
): string {
	if (!apiBaseURL) {
		return policy;
	}
	const apiOrigin = new URL(apiBaseURL).origin;
	if (apiOrigin === documentOrigin) {
		return policy;
	}
	return withConnectSource(policy, apiOrigin);
}
