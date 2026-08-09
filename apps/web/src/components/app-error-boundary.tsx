import * as React from "react";
import { FormattedMessage } from "react-intl";

type State = { failed: boolean };

export class AppErrorBoundary extends React.Component<
	{ children: React.ReactNode },
	State
> {
	override state: State = { failed: false };

	static getDerivedStateFromError(): State {
		return { failed: true };
	}

	override componentDidCatch(error: unknown) {
		console.error("Application boundary caught an error", error);
	}

	override render() {
		if (this.state.failed) {
			return (
				<main id="main-content">
					<h1>
						<FormattedMessage
							id="shell.boundary.title"
							defaultMessage="The shell could not start"
						/>
					</h1>
					<p role="alert">
						<FormattedMessage
							id="shell.boundary.message"
							defaultMessage="Reload the page to try again. No diagnostic details have been displayed."
						/>
					</p>
					<button type="button" onClick={() => location.reload()}>
						<FormattedMessage id="shell.reload" defaultMessage="Reload" />
					</button>
				</main>
			);
		}
		return this.props.children;
	}
}
