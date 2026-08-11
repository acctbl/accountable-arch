import { Button } from "@accountable/ui/components/button";
import { PlusIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import * as React from "react";
import { FormattedMessage, useIntl } from "react-intl";
import { PRODUCT_NAME } from "@/brand";
import { LocaleSwitcher } from "@/components/locale-switcher";
import { ThemeSwitcher } from "@/components/theme-switcher";
import { useApi } from "@/lib/api";
import { formatApiError } from "@/lib/api-error";

export const Route = createFileRoute("/")({ component: Home });

function Home() {
	const api = useApi();
	const intl = useIntl();
	const runtime = useQuery({
		queryKey: ["system", "runtime"],
		queryFn: () => {
			if (!api) {
				throw new Error("API clients are unavailable");
			}
			return api.system.getRuntime({});
		},
		enabled: api !== null,
		retry: false,
	});

	React.useEffect(() => {
		if (runtime.data || runtime.error) {
			performance.mark("accountable:route-useful-content");
		}
	}, [runtime.data, runtime.error]);

	return (
		<main id="main-content" className="flex min-h-svh" tabIndex={-1}>
			<div className="min-w-0 space-y-2 p-6">
				<h1 className="sr-only">{PRODUCT_NAME}</h1>
				<div className="flex items-center gap-1">
					<LocaleSwitcher />
					<ThemeSwitcher />
					<Button
						size="icon"
						variant="ghost"
						aria-label={intl.formatMessage({
							id: "home.add.label",
							defaultMessage: "Add",
						})}
					>
						<PlusIcon aria-hidden />
					</Button>
				</div>
				<p>
					<FormattedMessage
						id="home.description"
						defaultMessage="Local transport shell"
					/>
				</p>

				<p role="status" aria-live="polite" data-testid="runtime-status">
					{api === null || runtime.isPending ? (
						<FormattedMessage
							id="runtime.loading"
							defaultMessage="Connecting to the local API…"
						/>
					) : null}
					{runtime.data ? (
						<FormattedMessage
							id="runtime.connected"
							defaultMessage="Connected ({releaseId})"
							values={{ releaseId: runtime.data.releaseId }}
						/>
					) : null}
					{runtime.error ? formatApiError(intl, runtime.error) : null}
				</p>
			</div>
		</main>
	);
}
