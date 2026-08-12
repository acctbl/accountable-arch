package telemetry

import "sort"

var RegisteredLogEvents = map[string]struct{}{
	"runtime.starting":                 {},
	"runtime.ready":                    {},
	"runtime.stopping":                 {},
	"runtime.task_completed":           {},
	"runtime.configuration_failed":     {},
	"request.completed":                {},
	"request.failed":                   {},
	"request.panic_recovered":          {},
	"database.operation_failed":        {},
	"http.client.failed":               {},
	"identity.generation_failed":       {},
	"telemetry.export_failed":          {},
	"telemetry.sli_export_failed":      {},
	"telemetry.sli_delivery_uncertain": {},
	"telemetry.client_event_dropped":   {},
	"observability.proof":              {},
	"telemetry.accounting_uncertain":   {},
	"telemetry.task_uncertain":         {},
	"client.event":                     {},
}

// RegisteredResourceAttributes is the closed resource identity that every managed
// process emits and the Collector keeps.
var RegisteredResourceAttributes = []string{
	"service.name",
	"service.version",
	"deployment.environment.name",
	"accountable.cell.id",
	"accountable.runtime.role",
	"accountable.release.id",
	"accountable.configuration.schema_version",
	"accountable.configuration.revision",
	"accountable.configuration.fingerprint",
	"accountable.telemetry.protocol",
	"accountable.telemetry.sampling.policy",
	"accountable.telemetry.redaction.policy.version",
}

// RegisteredRuntimeIdentityAttributes is the bounded runtime identity that the
// Grafana metrics pipeline copies from the resource to series labels.
var RegisteredRuntimeIdentityAttributes = []string{
	"service.name",
	"service.version",
	"deployment.environment.name",
	"accountable.cell.id",
	"accountable.runtime.role",
	"accountable.release.id",
	"accountable.configuration.schema_version",
	"accountable.configuration.revision",
	"accountable.configuration.fingerprint",
}

// RegisteredWorkIdentityAttributes may appear on logs and spans and never on metrics.
var RegisteredWorkIdentityAttributes = []string{
	"request_id",
	"correlation_id",
	"operation_id",
	"event_id",
	"problem_id",
	"causation_id",
}

// RegisteredDescriptorAttributes is the closed set of bounded descriptors that
// application code may attach to a log or span.
var RegisteredDescriptorAttributes = []string{
	"action_code",
	"drop_count",
	"error_category",
	"event.name",
	"event_name",
	"module",
	"operation_class",
	"outcome",
	"proof_type",
	"reason",
	"route_class",
	"rpc_method",
	"rpc_service",
}

var registeredMetricAttributes = map[string]map[string]struct{}{
	"accountable.server.request.count":       attributeSet("route_class", "rpc_service", "rpc_method", "outcome", "error_category"),
	"accountable.server.request.duration":    attributeSet("route_class", "rpc_service", "rpc_method", "outcome"),
	"accountable.server.request.active":      attributeSet("route_class"),
	"accountable.db.operation.count":         attributeSet("operation_class", "outcome", "error_category"),
	"accountable.db.operation.duration":      attributeSet("operation_class", "outcome"),
	"accountable.client.event.count":         attributeSet("event_name", "route_class", "outcome", "error_category", "client_application", "release_id", "client_artifact_type", "client_artifact_digest", "configuration_schema_version", "configuration_revision", "configuration_fingerprint"),
	"accountable.client.duration":            attributeSet("event_name", "route_class", "outcome", "client_application", "release_id", "client_artifact_type", "client_artifact_digest", "configuration_schema_version", "configuration_revision", "configuration_fingerprint"),
	"accountable.browser.web_vital.duration": attributeSet("event_name", "route_class", "client_application", "release_id", "client_artifact_type", "client_artifact_digest", "configuration_schema_version", "configuration_revision", "configuration_fingerprint"),
	"accountable.browser.web_vital.cls":      attributeSet("route_class", "client_application", "release_id", "client_artifact_type", "client_artifact_digest", "configuration_schema_version", "configuration_revision", "configuration_fingerprint"),
	"accountable.observability.proof":        attributeSet("proof_type"),
	"accountable.sli.request.eligible":       {},
	"accountable.sli.availability.bad":       {},
	"accountable.sli.latency.slow":           {},
	"accountable.sli.export.heartbeat":       {},
}

var workIdentityFields = attributeSet(
	"request_id",
	"correlation_id",
	"operation_id",
	"event_id",
	"problem_id",
	"causation_id",
	"trace_id",
	"span_id",
)

type SignalFixture struct {
	Kind       string         `json:"kind"`
	Name       string         `json:"name"`
	Attributes map[string]any `json:"attributes"`
}

func ParseSignalFixture(raw []byte) (SignalFixture, error) {
	var signal SignalFixture
	if err := decodeStrictJSON(raw, &signal); err != nil {
		return SignalFixture{}, err
	}
	if err := ValidateSignalFixture(signal); err != nil {
		return SignalFixture{}, err
	}
	return signal, nil
}

func ValidateSignalFixture(signal SignalFixture) error {
	switch signal.Kind {
	case "log":
		if _, ok := RegisteredLogEvents[signal.Name]; !ok {
			return contractError("log event")
		}
	case "metric":
		return ValidateMetricAttributes(signal.Name, signal.Attributes)
	case "trace":
		for name := range signal.Attributes {
			if name == "trace_id" || name == "span_id" {
				return contractError("native trace identity")
			}
		}
	default:
		return contractError("signal kind")
	}
	return nil
}

func ValidateMetricAttributes(instrument string, attributes map[string]any) error {
	allowed, ok := registeredMetricAttributes[instrument]
	if !ok {
		return contractError("metric instrument")
	}
	for name := range attributes {
		if _, workIdentity := workIdentityFields[name]; workIdentity {
			return contractError("metric work identity")
		}
		if _, permitted := allowed[name]; !permitted {
			return contractError("metric attribute")
		}
	}
	return nil
}

// RegisteredSignalAttributes reports the sorted allow-list for one signal kind.
func RegisteredSignalAttributes(kind string) ([]string, error) {
	switch kind {
	case "log", "trace":
		names := append([]string{}, RegisteredWorkIdentityAttributes...)
		names = append(names, RegisteredDescriptorAttributes...)
		sort.Strings(names)
		return names, nil
	case "metric":
		unique := make(map[string]struct{})
		for _, allowed := range registeredMetricAttributes {
			for name := range allowed {
				unique[name] = struct{}{}
			}
		}
		for _, name := range RegisteredRuntimeIdentityAttributes {
			unique[name] = struct{}{}
		}
		names := make([]string, 0, len(unique))
		for name := range unique {
			names = append(names, name)
		}
		sort.Strings(names)
		return names, nil
	default:
		return nil, contractError("signal kind")
	}
}

func attributeSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}
