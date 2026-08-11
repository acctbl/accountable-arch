import { describe, expect, it } from "vitest";
import {
	nextThemeOverride,
	resolveTheme,
	themeFromOverride,
} from "./theme-bootstrap";

describe("theme override model", () => {
	it("resolves a stored override over the system look", () => {
		expect(resolveTheme("dark", "light")).toBe("dark");
		expect(resolveTheme(null, "light")).toBe("light");
	});

	it("maps a missing override to system in the theme model", () => {
		expect(themeFromOverride(null)).toBe("system");
		expect(themeFromOverride("dark")).toBe("dark");
	});

	it("toggles to an override when the opposite differs from the OS", () => {
		expect(nextThemeOverride("light", "light")).toBe("dark");
		expect(nextThemeOverride("dark", "dark")).toBe("light");
	});

	it("clears the override when the opposite matches the OS", () => {
		expect(nextThemeOverride("dark", "light")).toBe(null);
		expect(nextThemeOverride("light", "dark")).toBe(null);
	});
});
