import * as React from "react";
import {
	COLOR_SCHEME_QUERY,
	isThemeOverride,
	nextThemeOverride,
	type ResolvedTheme,
	resolveTheme,
	THEME_STORAGE_KEY,
	type Theme,
	type ThemeOverride,
	themeFromOverride,
} from "./theme-bootstrap";

type ThemeProviderProps = {
	children: React.ReactNode;
	storageKey?: string;
	disableTransitionOnChange?: boolean;
};

type ThemeProviderState = {
	theme: Theme;
	resolvedTheme: ResolvedTheme;
	setTheme: (theme: Theme) => void;
	toggleTheme: () => void;
};

const ThemeProviderContext = React.createContext<
	ThemeProviderState | undefined
>(undefined);

function storageArea(): Storage | null {
	try {
		return window.localStorage;
	} catch {
		return null;
	}
}

function readOverride(storageKey: string): ThemeOverride | null {
	const value = storageArea()?.getItem(storageKey) ?? null;
	if (isThemeOverride(value)) {
		return value;
	}

	if (value !== null) {
		try {
			storageArea()?.removeItem(storageKey);
		} catch {
			// Stale values stay unread as system.
		}
	}

	return null;
}

function writeOverride(storageKey: string, override: ThemeOverride | null) {
	const storage = storageArea();
	if (!storage) {
		return;
	}

	try {
		if (override === null) {
			storage.removeItem(storageKey);
			return;
		}

		storage.setItem(storageKey, override);
	} catch {
		// A refused write only loses the stored preference.
	}
}

function getSystemTheme(): ResolvedTheme {
	if (window.matchMedia(COLOR_SCHEME_QUERY).matches) {
		return "dark";
	}

	return "light";
}

function applyResolvedTheme(
	resolved: ResolvedTheme,
	disableTransitionOnChange: boolean,
) {
	const root = document.documentElement;
	const restoreTransitions = disableTransitionOnChange
		? disableTransitionsTemporarily()
		: null;

	root.classList.remove("light", "dark");
	root.classList.add(resolved);

	if (restoreTransitions) {
		restoreTransitions();
	}
}

function disableTransitionsTemporarily() {
	const style = document.createElement("style");
	style.appendChild(
		document.createTextNode(
			"*,*::before,*::after{-webkit-transition:none!important;transition:none!important}",
		),
	);
	document.head.appendChild(style);

	return () => {
		window.getComputedStyle(document.body);
		requestAnimationFrame(() => {
			requestAnimationFrame(() => {
				style.remove();
			});
		});
	};
}

export function ThemeProvider({
	children,
	storageKey = THEME_STORAGE_KEY,
	disableTransitionOnChange = true,
	...props
}: ThemeProviderProps) {
	const [override, setOverride] = React.useState<ThemeOverride | null>(() =>
		readOverride(storageKey),
	);
	const [systemTheme, setSystemTheme] =
		React.useState<ResolvedTheme>(getSystemTheme);

	const resolvedTheme = resolveTheme(override, systemTheme);
	const theme = themeFromOverride(override);

	React.useLayoutEffect(() => {
		applyResolvedTheme(resolvedTheme, disableTransitionOnChange);
	}, [disableTransitionOnChange, resolvedTheme]);

	const commitOverride = React.useCallback(
		(nextOverride: ThemeOverride | null) => {
			const system = getSystemTheme();
			writeOverride(storageKey, nextOverride);
			setSystemTheme(system);
			setOverride(nextOverride);
		},
		[storageKey],
	);

	const setTheme = React.useCallback(
		(nextTheme: Theme) => {
			commitOverride(nextTheme === "system" ? null : nextTheme);
		},
		[commitOverride],
	);

	const toggleTheme = React.useCallback(() => {
		const system = getSystemTheme();
		const resolved = resolveTheme(override, system);
		commitOverride(nextThemeOverride(resolved, system));
	}, [commitOverride, override]);

	React.useEffect(() => {
		if (override !== null) {
			return undefined;
		}

		const mediaQuery = window.matchMedia(COLOR_SCHEME_QUERY);
		const handleChange = () => {
			const nextSystem = getSystemTheme();
			setSystemTheme(nextSystem);
		};

		mediaQuery.addEventListener("change", handleChange);

		return () => {
			mediaQuery.removeEventListener("change", handleChange);
		};
	}, [override]);

	React.useEffect(() => {
		const handleStorageChange = (event: StorageEvent) => {
			if (event.storageArea !== storageArea()) {
				return;
			}

			if (event.key !== storageKey) {
				return;
			}

			const nextOverride = isThemeOverride(event.newValue)
				? event.newValue
				: null;
			setSystemTheme(getSystemTheme());
			setOverride(nextOverride);
		};

		window.addEventListener("storage", handleStorageChange);

		return () => {
			window.removeEventListener("storage", handleStorageChange);
		};
	}, [storageKey]);

	const value = React.useMemo(
		() => ({
			theme,
			resolvedTheme,
			setTheme,
			toggleTheme,
		}),
		[resolvedTheme, setTheme, theme, toggleTheme],
	);

	return (
		<ThemeProviderContext.Provider {...props} value={value}>
			{children}
		</ThemeProviderContext.Provider>
	);
}

export function useTheme() {
	const context = React.useContext(ThemeProviderContext);

	if (context === undefined) {
		throw new Error("useTheme must be used within a ThemeProvider");
	}

	return context;
}
