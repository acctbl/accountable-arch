import { Button } from "@accountable/ui/components/button";
import { MoonIcon, SunIcon } from "@phosphor-icons/react";
import { defineMessages, useIntl } from "react-intl";
import { useTheme } from "./theme-provider";

const messages = defineMessages({
	toDark: {
		id: "themeSwitcher.toDark",
		defaultMessage: "Switch to dark appearance",
	},
	toLight: {
		id: "themeSwitcher.toLight",
		defaultMessage: "Switch to light appearance",
	},
});

export function ThemeSwitcher({ className }: { className?: string }) {
	const { resolvedTheme, toggleTheme } = useTheme();
	const intl = useIntl();
	const nextTheme = resolvedTheme === "dark" ? "light" : "dark";

	return (
		<Button
			type="button"
			size="icon"
			variant="ghost"
			className={className}
			onClick={toggleTheme}
			aria-label={intl.formatMessage(
				nextTheme === "dark" ? messages.toDark : messages.toLight,
			)}
		>
			{nextTheme === "dark" ? (
				<MoonIcon aria-hidden />
			) : (
				<SunIcon aria-hidden />
			)}
		</Button>
	);
}
