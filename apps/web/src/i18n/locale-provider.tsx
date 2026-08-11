import * as React from "react";
import {
	type CatalogLoaders,
	catalogPath,
	type Messages,
	mergeCatalogs,
} from "./catalog";
import {
	DEFAULT_LOCALE,
	getDirection,
	type Locale,
	SOURCE_LOCALE,
} from "./config";
import {
	negotiate,
	readFormattingLocalePreference,
	updateLocaleInUrl,
	writeFormattingLocaleCookie,
	writeLocaleCookie,
} from "./negotiate";

export type LocaleBundle = {
	locale: Locale;
	messages: Messages;
};

export type InitialLocaleState = {
	bundle: LocaleBundle;
	formattingLocalePreference: Locale | null;
	loadError: Error | null;
	hydrate?: boolean;
};

type LocaleProviderState = LocaleBundle & {
	formattingLocale: Locale;
	pendingLocale: Locale | null;
	loadError: Error | null;
	setLocale: (locale: Locale) => void;
	setFormattingLocale: (locale: Locale) => void;
};

type LocaleBundleLoader = (locale: Locale) => Promise<LocaleBundle>;

const bundledSourceCatalog = import.meta.glob<Messages>("./compiled/en.json", {
	eager: true,
	import: "default",
});

const sourceMessages: Messages =
	bundledSourceCatalog[catalogPath(SOURCE_LOCALE)] ?? {};

const lazyCatalogs: CatalogLoaders<Messages[string]> = {
	...import.meta.glob<Messages>(
		["./compiled/*.json", "!./compiled/en.json", "!./compiled/en-X*.json"],
		{ import: "default" },
	),
	...(import.meta.env.DEV
		? import.meta.glob<Messages>("./compiled/en-X*.json", {
				import: "default",
			})
		: {}),
};

const bundleCache = new Map<Locale, Promise<LocaleBundle>>();

const sourceBundle: LocaleBundle = {
	locale: SOURCE_LOCALE,
	messages: sourceMessages,
};

bundleCache.set(SOURCE_LOCALE, Promise.resolve(sourceBundle));

const LocaleProviderContext = React.createContext<
	LocaleProviderState | undefined
>(undefined);

function asError(cause: unknown): Error {
	return cause instanceof Error ? cause : new Error(String(cause));
}

export function applyDocumentLocale(locale: Locale) {
	const root = document.documentElement;

	root.lang = locale;
	root.dir = getDirection(locale);
}

export function loadLocaleBundle(locale: Locale): Promise<LocaleBundle> {
	const cached = bundleCache.get(locale);
	if (cached) {
		return cached;
	}

	const loading = mergeCatalogs(locale, sourceMessages, lazyCatalogs)
		.then((messages) => ({ locale, messages }))
		.catch((cause: unknown) => {
			bundleCache.delete(locale);
			throw cause;
		});

	bundleCache.set(locale, loading);

	return loading;
}

export async function loadInitialLocaleState(): Promise<InitialLocaleState> {
	const locale = negotiate();
	const formattingLocalePreference = readFormattingLocalePreference();

	try {
		return {
			bundle: await loadLocaleBundle(locale),
			formattingLocalePreference,
			loadError: null,
		};
	} catch (cause) {
		return {
			bundle: { locale: DEFAULT_LOCALE, messages: sourceMessages },
			formattingLocalePreference,
			loadError: asError(cause),
		};
	}
}

export function createSyncInitialLocaleState(): InitialLocaleState {
	return {
		bundle: {
			locale: negotiate(),
			messages: sourceMessages,
		},
		formattingLocalePreference: readFormattingLocalePreference(),
		loadError: null,
		hydrate: true,
	};
}

export function LocaleProvider({
	children,
	initialState,
	loadBundle = loadLocaleBundle,
}: {
	children: React.ReactNode;
	initialState: InitialLocaleState;
	loadBundle?: LocaleBundleLoader;
}) {
	const [bundle, setBundle] = React.useState(initialState.bundle);
	const [formattingLocalePreference, setFormattingLocalePreference] =
		React.useState(initialState.formattingLocalePreference);
	const [pendingLocale, setPendingLocale] = React.useState<Locale | null>(null);
	const [loadError, setLoadError] = React.useState(initialState.loadError);
	const activeLocale = React.useRef(bundle.locale);
	const latestRequest = React.useRef(0);
	const formattingLocale = formattingLocalePreference ?? bundle.locale;

	React.useLayoutEffect(() => {
		activeLocale.current = bundle.locale;
		applyDocumentLocale(bundle.locale);
	}, [bundle.locale]);

	React.useEffect(() => {
		if (!initialState.hydrate) {
			return undefined;
		}

		const request = ++latestRequest.current;
		const locale = initialState.bundle.locale;

		void loadBundle(locale)
			.then((loaded) => {
				if (latestRequest.current !== request) {
					return;
				}

				setBundle(loaded);
				setLoadError(null);
			})
			.catch((cause: unknown) => {
				if (latestRequest.current !== request) {
					return;
				}

				const error = asError(cause);
				console.error("Failed to load locale catalog", error);
				setLoadError(error);
			});

		return undefined;
	}, [initialState.bundle.locale, initialState.hydrate, loadBundle]);

	const setLocale = React.useCallback(
		(nextLocale: Locale) => {
			const request = ++latestRequest.current;

			if (nextLocale === activeLocale.current) {
				setPendingLocale(null);
				setLoadError(null);
				writeLocaleCookie(nextLocale);
				updateLocaleInUrl(nextLocale);
				return;
			}

			setPendingLocale(nextLocale);
			setLoadError(null);

			void loadBundle(nextLocale)
				.then((loaded) => {
					if (latestRequest.current !== request) {
						return;
					}

					writeLocaleCookie(loaded.locale);
					updateLocaleInUrl(loaded.locale);
					setBundle(loaded);
					setPendingLocale(null);
				})
				.catch((cause: unknown) => {
					if (latestRequest.current !== request) {
						return;
					}

					const error = asError(cause);
					console.error("Failed to load locale catalog", error);
					setPendingLocale(null);
					setLoadError(error);
				});
		},
		[loadBundle],
	);

	const setFormattingLocale = React.useCallback((nextLocale: Locale) => {
		writeFormattingLocaleCookie(nextLocale);
		setFormattingLocalePreference(nextLocale);
	}, []);

	const value = React.useMemo(
		() => ({
			...bundle,
			formattingLocale,
			pendingLocale,
			loadError,
			setLocale,
			setFormattingLocale,
		}),
		[
			bundle,
			formattingLocale,
			pendingLocale,
			loadError,
			setLocale,
			setFormattingLocale,
		],
	);

	return (
		<LocaleProviderContext.Provider value={value}>
			{children}
		</LocaleProviderContext.Provider>
	);
}

export const useLocale = () => {
	const context = React.useContext(LocaleProviderContext);

	if (context === undefined) {
		throw new Error("useLocale must be used within a LocaleProvider");
	}

	return context;
};
