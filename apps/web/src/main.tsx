import { getSerwist } from "virtual:serwist";
import { DirectionProvider } from "@base-ui/react/direction-provider";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createRouter, RouterProvider } from "@tanstack/react-router";
import * as React from "react";
import ReactDOM from "react-dom/client";
import { FormattedMessage, IntlProvider } from "react-intl";
import { AppErrorBoundary } from "@/components/app-error-boundary";
import { ThemeProvider } from "@/components/theme-provider";
import { getDirection, SOURCE_LOCALE } from "@/i18n/config";
import {
	applyDocumentLocale,
	createSyncInitialLocaleState,
	type InitialLocaleState,
	LocaleProvider,
	useLocale,
} from "@/i18n/locale-provider";
import { RegionalSettingsProvider } from "@/i18n/regional-settings";
import { type ApiClients, ApiProvider, createApiClients } from "@/lib/api";
import { initializeClientTelemetry } from "@/lib/client-telemetry";
import {
	type LoadedRuntimeConfig,
	loadRuntimeConfig,
	peekRuntimeConfig,
	type RuntimeConfig,
} from "@/lib/runtime-config";
import { routeTree } from "./routeTree.gen";
import "@/styles.css";

const router = createRouter({
	routeTree,
	defaultPreload: "intent",
	scrollRestoration: true,
});

declare module "@tanstack/react-router" {
	interface Register {
		router: typeof router;
	}
}

function ConfigurationUnavailable() {
	return (
		<main id="main-content" className="mx-auto max-w-3xl px-6 py-16">
			<h1>
				<FormattedMessage
					defaultMessage="Configuration unavailable"
					id="app.configurationUnavailable.title"
				/>
			</h1>
			<p role="alert">
				<FormattedMessage
					defaultMessage="The app could not load its public runtime configuration."
					id="app.configurationUnavailable.message"
				/>
			</p>
			<button type="button" onClick={() => location.reload()}>
				<FormattedMessage
					defaultMessage="Reload"
					id="app.configurationUnavailable.reload"
				/>
			</button>
		</main>
	);
}

function Shell({
	clients,
	configFailed,
	queryClient,
}: {
	clients: ApiClients | null;
	configFailed: boolean;
	queryClient: QueryClient;
}) {
	const { locale, messages } = useLocale();
	const regionalSettings = React.useMemo(
		() => ({
			timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
			currency: null,
			jurisdiction: null,
		}),
		[],
	);
	React.useEffect(() => {
		performance.mark("accountable:application-ready");
	}, []);

	return (
		<IntlProvider
			locale={locale}
			defaultLocale={SOURCE_LOCALE}
			messages={messages}
		>
			<DirectionProvider direction={getDirection(locale)}>
				<AppErrorBoundary>
					<ThemeProvider>
						<RegionalSettingsProvider value={regionalSettings}>
							<ApiProvider clients={clients}>
								<QueryClientProvider client={queryClient}>
									{configFailed ? (
										<ConfigurationUnavailable />
									) : (
										<RouterProvider router={router} />
									)}
								</QueryClientProvider>
							</ApiProvider>
						</RegionalSettingsProvider>
					</ThemeProvider>
				</AppErrorBoundary>
			</DirectionProvider>
		</IntlProvider>
	);
}

function Boot({ initialLocale }: { initialLocale: InitialLocaleState }) {
	const [runtimeConfig, setRuntimeConfig] =
		React.useState<RuntimeConfig | null>(peekRuntimeConfig);
	const [configFailed, setConfigFailed] = React.useState(false);
	const queryClient = React.useMemo(
		() =>
			new QueryClient({
				defaultOptions: { queries: { retry: false, staleTime: 30_000 } },
			}),
		[],
	);
	const clients = React.useMemo(
		() => (runtimeConfig ? createApiClients(runtimeConfig) : null),
		[runtimeConfig],
	);

	React.useEffect(() => {
		void loadRuntimeConfig()
			.then((loaded: LoadedRuntimeConfig) => {
				setRuntimeConfig(loaded);
				void initializeClientTelemetry(loaded);
			})
			.catch(() => {
				if (!runtimeConfig) setConfigFailed(true);
			});
	}, []);

	React.useEffect(() => {
		if (!runtimeConfig?.architecture_probe) {
			return;
		}

		void import("./lib/architecture-probe-bridge").then(
			({ installArchitectureProbeBridge }) => {
				installArchitectureProbeBridge(runtimeConfig);
			},
		);
	}, [runtimeConfig]);

	return (
		<LocaleProvider initialState={initialLocale}>
			<Shell
				clients={clients}
				configFailed={configFailed}
				queryClient={queryClient}
			/>
		</LocaleProvider>
	);
}

async function registerSerwist() {
	if (
		import.meta.env.DEV ||
		location.hostname === "localhost" ||
		location.hostname === "127.0.0.1" ||
		!("serviceWorker" in navigator)
	) {
		return;
	}

	const serwist = await getSerwist();
	await serwist?.register();
}

void registerSerwist();

function mountApp() {
	const rootElement = document.getElementById("root");
	if (!rootElement || rootElement.innerHTML) {
		return;
	}

	const initialLocale = createSyncInitialLocaleState();
	applyDocumentLocale(initialLocale.bundle.locale);

	ReactDOM.createRoot(rootElement).render(
		<Boot initialLocale={initialLocale} />,
	);
}

mountApp();
