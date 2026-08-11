import { themeBootstrapScript } from "./components/theme-bootstrap";
import { localeBootstrapScript } from "./i18n/bootstrap";

export function documentBootstrapScript(includePseudoLocales = false): string {
	return [
		themeBootstrapScript(),
		localeBootstrapScript(includePseudoLocales),
	].join("\n");
}
