package telemetry

const (
	ContractSchemaVersion       = 1
	GoVersion                   = "1.26.5"
	ClientTelemetryVersion      = "0.1.0"
	ValibotVersion              = "1.4.2"
	CollectorVersion            = "0.158.0"
	CollectorStableVersion      = "1.64.0"
	OpenTelemetryBuilderVersion = "0.158.0"
	CosignVersion               = "v3.0.6"
	JSONCanonicalizationVersion = "v1.0.1"
	TelemetryProtocol           = "otlp_http"
	TelemetrySamplingPolicy     = "parent_based_5pct_v1"
	TelemetryRedactionPolicy    = "allowlist_v1"
	GitHubActionsOIDCIssuer     = "https://token.actions.githubusercontent.com"
)

const (
	// GrafanaStackSlug is the immutable slug of the one shared stack. It is the
	// identity that appears in the stack URL and in every stack-scoped alert.
	GrafanaStackSlug = "acctbl"
)

const (
	ScratchDirectory         = "/run/accountable"
	CollectorApplicationPort = 4318
	CollectorSLIPort         = 4319
	CollectorInternalPort    = 8888
)
