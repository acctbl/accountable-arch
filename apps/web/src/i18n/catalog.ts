import type { MessageFormatElement } from "react-intl";
import { type Locale, languageOf } from "./config";

export type Messages = Record<string, MessageFormatElement[]>;

export type CatalogLoaders<T> = Record<
	string,
	() => Promise<Record<string, T>>
>;

export function catalogPath(locale: string): string {
	return `./compiled/${locale}.json`;
}

export function localeChainLeastToMostSpecific(locale: Locale): string[] {
	return [...new Set([languageOf(locale), locale])];
}

export async function mergeCatalogs<T>(
	locale: Locale,
	base: Record<string, T>,
	loaders: CatalogLoaders<T>,
): Promise<Record<string, T>> {
	const merged: Record<string, T> = { ...base };
	const catalogs = await Promise.all(
		localeChainLeastToMostSpecific(locale)
			.map((tag) => loaders[catalogPath(tag)])
			.filter((loadCatalog) => loadCatalog !== undefined)
			.map((loadCatalog) => loadCatalog()),
	);

	for (const catalog of catalogs) {
		Object.assign(merged, catalog);
	}

	return merged;
}
