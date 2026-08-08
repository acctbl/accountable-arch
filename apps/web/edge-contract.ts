import { readFileSync } from "node:fs";

export const cacheControl = {
	runtimeConfig: "no-store",
	hashedAsset: "public, max-age=31536000, immutable",
	document: "no-cache",
} as const;

export const contentSecurityPolicy = readFileSync(
	new URL(
		"../../infra/opentofu/modules/cell/content-security-policy.txt",
		import.meta.url,
	),
	"utf8",
).trim();

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
