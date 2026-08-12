import { SystemService } from "@accountable/proto/accountable/system/v1/system_pb";
import {
	type Client,
	Code,
	ConnectError,
	createClient,
	type Interceptor,
	type Transport,
} from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import * as React from "react";
import { journeyCorrelationId, recordAPIRequest } from "./client-telemetry";
import type { RuntimeConfig } from "./runtime-config";

export type ApiClients = {
	system: Client<typeof SystemService>;
};

const ApiContext = React.createContext<ApiClients | null>(null);

function correlationAndLocaleInterceptor(): Interceptor {
	return (next) => async (request) => {
		const activeLocale = document.documentElement.lang;
		if (activeLocale) {
			request.header.set("Accept-Language", activeLocale);
		}
		if (!request.header.has("X-Correlation-ID")) {
			request.header.set("X-Correlation-ID", journeyCorrelationId());
		}
		const started = performance.now();
		try {
			const response = await next(request);
			void recordAPIRequest("2xx", "success", "none", started);
			return response;
		} catch (error) {
			const connectError = ConnectError.from(error);
			const timeout = connectError.code === Code.DeadlineExceeded;
			const server =
				connectError.code === Code.Internal ||
				connectError.code === Code.Unavailable;
			const statusClass =
				timeout || connectError.code === Code.Unknown
					? "network"
					: server
						? "5xx"
						: "4xx";
			const category = timeout
				? "timeout"
				: statusClass === "network"
					? "network"
					: server
						? "server"
						: "client";
			void recordAPIRequest(
				statusClass,
				statusClass === "5xx" || statusClass === "network"
					? "failure"
					: "success",
				category,
				started,
			);
			throw error;
		}
	};
}

export function createApiTransport(config: RuntimeConfig): Transport {
	const includeCredentials: typeof fetch = (input, init) =>
		fetch(input, { ...init, credentials: "include" });
	return createConnectTransport({
		baseUrl: config.api_base_url,
		useBinaryFormat: true,
		fetch: includeCredentials,
		interceptors: [correlationAndLocaleInterceptor()],
	});
}

export function createApiClients(config: RuntimeConfig): ApiClients {
	const transport = createApiTransport(config);
	return {
		system: createClient(SystemService, transport),
	};
}

export function ApiProvider({
	children,
	clients,
}: {
	children: React.ReactNode;
	clients: ApiClients | null;
}) {
	return <ApiContext.Provider value={clients}>{children}</ApiContext.Provider>;
}

export function useApi(): ApiClients | null {
	return React.useContext(ApiContext);
}
