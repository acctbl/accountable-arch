package telemetry

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/acctbl/accountable/internal/platform/clock"
)

// SafeLogger writes registered events to stdout and to the OpenTelemetry log
// provider. The stdout path stays useful during complete Collector or Grafana
// failure, so it never depends on an exporter.
type SafeLogger struct {
	stdout   *slog.Logger
	bridge   *slog.Logger
	resource []slog.Attr
	clock    clock.Clock

	limitMutex  sync.Mutex
	lastLimited map[string]time.Time
}

// SafeLoggerOptions builds one process logger.
type SafeLoggerOptions struct {
	Stdout   io.Writer
	Resource ProcessResource
	Bridge   *slog.Logger
	Clock    clock.Clock
}

// RateLimitedEvents may emit at most one record for each process in one minute.
var RateLimitedEvents = map[string]time.Duration{
	"identity.generation_failed": time.Minute,
}

func NewSafeLogger(options SafeLoggerOptions) *SafeLogger {
	source := options.Clock
	if source == nil {
		source = clock.System{}
	}
	handler := slog.NewJSONHandler(options.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	return &SafeLogger{
		stdout:      slog.New(handler),
		bridge:      options.Bridge,
		resource:    resourceAttrs(options.Resource),
		clock:       source,
		lastLimited: make(map[string]time.Time),
	}
}

// Emit writes one registered event with its resource identity, work identity, and
// bounded attributes. An unregistered event or attribute never reaches an output.
func (logger *SafeLogger) Emit(ctx context.Context, level slog.Level, eventName string, attributes map[string]string) error {
	if _, registered := RegisteredLogEvents[eventName]; !registered {
		return contractError("log event")
	}
	allowed, err := RegisteredSignalAttributes("log")
	if err != nil {
		return err
	}
	for name := range attributes {
		if !contains(allowed, name) {
			return contractError("log attribute")
		}
	}
	if !logger.admit(eventName) {
		return nil
	}
	provided := make([]slog.Attr, 0, len(attributes))
	for name, value := range attributes {
		provided = append(provided, slog.String(name, value))
	}
	logger.emit(ctx, level, eventName, provided)
	return nil
}

func (logger *SafeLogger) EmitClientDrop(ctx context.Context, reason string, count int64) error {
	if count <= 0 || (reason != "shutdown_timeout" && reason != "worker_failure" && reason != "invalid_internal_state") {
		return contractError("client drop")
	}
	logger.emit(ctx, slog.LevelError, "telemetry.client_event_dropped", []slog.Attr{
		slog.String(StdoutFieldReason, reason),
		slog.Int64(StdoutFieldDropCount, count),
	})
	return nil
}

func (logger *SafeLogger) emit(ctx context.Context, level slog.Level, eventName string, provided []slog.Attr) {
	fields := make([]slog.Attr, 0, len(logger.resource)+len(provided)+7)
	fields = append(fields, slog.String(StdoutFieldEventName, eventName))
	fields = append(fields, logger.resource...)
	if identity, ok := WorkIdentityFromContext(ctx); ok {
		for name, value := range WorkIdentityAttributes(identity) {
			fields = append(fields, slog.String(name, value))
		}
	}
	fields = append(fields, provided...)
	logger.stdout.LogAttrs(ctx, level, eventName, fields...)
	if logger.bridge != nil {
		logger.bridge.LogAttrs(ctx, level, eventName, fields...)
	}
}

// EmitPreVerification omits every field the process has not yet verified.
func (logger *SafeLogger) EmitPreVerification(ctx context.Context, eventName string, attributes map[string]string) error {
	if _, registered := RegisteredLogEvents[eventName]; !registered {
		return contractError("log event")
	}
	fields := []slog.Attr{slog.String(StdoutFieldEventName, eventName)}
	for name, value := range attributes {
		fields = append(fields, slog.String(name, value))
	}
	logger.stdout.LogAttrs(ctx, slog.LevelError, eventName, fields...)
	return nil
}

func (logger *SafeLogger) admit(eventName string) bool {
	window, limited := RateLimitedEvents[eventName]
	if !limited {
		return true
	}
	logger.limitMutex.Lock()
	defer logger.limitMutex.Unlock()
	now := logger.clock.Now()
	last, seen := logger.lastLimited[eventName]
	if seen && now.Sub(last) < window {
		return false
	}
	logger.lastLimited[eventName] = now
	return true
}

// Safe stdout records use flat field names. CloudWatch metric filters select
// JSON fields by path, and a dotted key is not addressable there, so the stdout
// projection of each dotted resource attribute uses an underscore name. These
// constants are the only names a metric filter may reference.
const (
	StdoutFieldEventName   = "event_name"
	StdoutFieldCellID      = "accountable_cell_id"
	StdoutFieldEnvironment = "deployment_environment_name"
	StdoutFieldReason      = "reason"
	StdoutFieldDropCount   = "drop_count"
)

// resourceAttrs projects the resource identity onto the flat field names a
// CloudWatch metric filter can select.
func resourceAttrs(resource ProcessResource) []slog.Attr {
	return []slog.Attr{
		slog.String("service_name", resource.ServiceName),
		slog.String("service_version", resource.ServiceVersion),
		slog.String(StdoutFieldEnvironment, resource.DeploymentEnvironmentName),
		slog.String(StdoutFieldCellID, resource.CellID),
		slog.String("accountable_runtime_role", resource.RuntimeRole),
		slog.String("accountable_release_id", resource.ReleaseID),
		slog.String("accountable_configuration_revision", resource.ConfigurationRevision),
		slog.String("accountable_configuration_fingerprint", resource.ConfigurationFingerprint),
		slog.String("accountable_telemetry_protocol", resource.TelemetryProtocol),
		slog.String("accountable_telemetry_sampling_policy", resource.TelemetrySamplingPolicy),
		slog.String("accountable_telemetry_redaction_policy_version", resource.TelemetryRedactionPolicy),
	}
}
