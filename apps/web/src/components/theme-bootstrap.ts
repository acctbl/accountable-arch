export const THEME_STORAGE_KEY = "acctbl-theme";
export const COLOR_SCHEME_QUERY = "(prefers-color-scheme: dark)";

export type ThemeOverride = "light" | "dark";
export type Theme = ThemeOverride | "system";
export type ResolvedTheme = ThemeOverride;

const THEME_OVERRIDES: readonly ThemeOverride[] = ["light", "dark"];

export function isThemeOverride(value: string | null): value is ThemeOverride {
	return value === "light" || value === "dark";
}

export function resolveTheme(
	override: ThemeOverride | null,
	system: ResolvedTheme,
): ResolvedTheme {
	return override ?? system;
}

export function nextThemeOverride(
	resolved: ResolvedTheme,
	system: ResolvedTheme,
): ThemeOverride | null {
	const opposite: ResolvedTheme = resolved === "dark" ? "light" : "dark";
	if (opposite === system) {
		return null;
	}

	return opposite;
}

export function themeFromOverride(override: ThemeOverride | null): Theme {
	return override ?? "system";
}

export function themeBootstrapScript(storageKey = THEME_STORAGE_KEY): string {
	const overrides = JSON.stringify(THEME_OVERRIDES);
	const key = JSON.stringify(storageKey);
	const colorSchemeQuery = JSON.stringify(COLOR_SCHEME_QUERY);

	return `
(() => {
  const overrides = ${overrides};
  const storageKey = ${key};
  let resolved = window.matchMedia(${colorSchemeQuery}).matches ? "dark" : "light";
  try {
    const stored = window.localStorage.getItem(storageKey);
    if (overrides.includes(stored)) {
      resolved = stored;
    } else if (stored !== null) {
      window.localStorage.removeItem(storageKey);
    }
  } catch {}
  const root = document.documentElement;
  root.classList.remove("light", "dark");
  root.classList.add(resolved);
})();
`.trim();
}
