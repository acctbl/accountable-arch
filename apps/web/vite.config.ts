import fs from "node:fs";
import { serwist } from "@serwist/vite";
import tailwindcss from "@tailwindcss/vite";

import { tanstackRouter } from "@tanstack/router-plugin/vite";

import viteReact from "@vitejs/plugin-react";
import { defineConfig, type Plugin } from "vite";
import {
	cacheControl,
	contentSecurityPolicy,
	withConnectSource,
} from "./edge-contract";
import { localeBootstrapScript } from "./src/i18n/bootstrap";

function localeBootstrap(includePseudoLocales: boolean): Plugin {
	return {
		name: "accountable:locale-bootstrap",
		transformIndexHtml() {
			return [
				{
					tag: "script",
					children: localeBootstrapScript(includePseudoLocales),
					injectTo: "head",
				},
			];
		},
	};
}

function runtimeConfigBody(): string {
	const apiBaseURL =
		process.env.ACCOUNTABLE_RUNTIME_API_BASE_URL ?? "http://localhost:8080";
	const architectureProbe =
		process.env.ACCOUNTABLE_RUNTIME_ARCHITECTURE_PROBE === "true";
	const configurationRevision =
		process.env.ACCOUNTABLE_RUNTIME_CONFIGURATION_REVISION ??
		"local-development";
	return JSON.stringify({
		schema_version: 1,
		api_base_url: apiBaseURL,
		architecture_probe: architectureProbe,
		configuration_revision: configurationRevision,
	});
}

function previewContentSecurityPolicy(): string {
	const apiBaseURL = process.env.ACCOUNTABLE_RUNTIME_API_BASE_URL;
	if (!apiBaseURL) {
		return contentSecurityPolicy;
	}
	return withConnectSource(contentSecurityPolicy, new URL(apiBaseURL).origin);
}

function edgeContractSimulator(): Plugin {
	const serveConfig = (
		_request: unknown,
		response: import("node:http").ServerResponse,
	) => {
		response.statusCode = 200;
		response.setHeader("Cache-Control", cacheControl.runtimeConfig);
		response.setHeader("Content-Type", "application/json; charset=utf-8");
		response.end(runtimeConfigBody());
	};
	return {
		name: "accountable:edge-contract-simulator",
		configureServer(server) {
			server.middlewares.use("/_runtime/config.json", serveConfig);
		},
		configurePreviewServer(server) {
			server.middlewares.use("/_runtime/config.json", serveConfig);
			server.middlewares.use((request, response, next) => {
				const path = (request.url ?? "").split("?")[0] ?? "";
				const hashedAsset = path.startsWith("/assets/");
				response.setHeader(
					"Cache-Control",
					hashedAsset ? cacheControl.hashedAsset : cacheControl.document,
				);
				if (!hashedAsset) {
					response.setHeader(
						"Content-Security-Policy",
						previewContentSecurityPolicy(),
					);
				}
				next();
			});
		},
	};
}

function previewHTTPS(): { key: Buffer; cert: Buffer } | undefined {
	const keyPath = process.env.ACCOUNTABLE_TLS_KEY_FILE;
	const certPath = process.env.ACCOUNTABLE_TLS_CERT_FILE;
	if (!keyPath || !certPath) {
		return undefined;
	}
	return {
		key: fs.readFileSync(keyPath),
		cert: fs.readFileSync(certPath),
	};
}

const config = defineConfig(({ command }) => {
	const includePseudoLocales = command === "serve";
	const https = previewHTTPS();

	return {
		resolve: { tsconfigPaths: true },
		server: {
			proxy: {
				"/accountable.system.v1.SystemService": {
					target: "http://localhost:8080",
				},
			},
		},
		preview: {
			host: process.env.ACCOUNTABLE_WEB_HOST ?? "127.0.0.1",
			port: Number(process.env.ACCOUNTABLE_WEB_PORT ?? "4173"),
			strictPort: true,
			https,
		},
		plugins: [
			edgeContractSimulator(),
			localeBootstrap(includePseudoLocales),
			tailwindcss(),
			tanstackRouter({ target: "react", autoCodeSplitting: true }),
			viteReact(),
			serwist({
				disable: command === "serve",
				swSrc: "src/sw.ts",
				swDest: "sw.js",
				globDirectory: "dist",
				injectionPoint: "self.__SW_MANIFEST",
				rollupFormat: "iife",
			}),
		],
	};
});

export default config;
