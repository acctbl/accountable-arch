package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/acctbl/accountable/internal/platform/clock"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func TestProofSamplerAlwaysSamplesOnlyAuthenticatedProofAttributes(t *testing.T) {
	sampler := proofSampler{normal: sdktrace.NeverSample()}
	proof := sampler.ShouldSample(sdktrace.SamplingParameters{Attributes: []attribute.KeyValue{
		attribute.String("proof_type", ProofType),
	}})
	if proof.Decision != sdktrace.RecordAndSample {
		t.Fatalf("proof decision = %v", proof.Decision)
	}
	normal := sampler.ShouldSample(sdktrace.SamplingParameters{Attributes: []attribute.KeyValue{
		attribute.String("event_name", "observability.proof"),
	}})
	if normal.Decision != sdktrace.Drop {
		t.Fatalf("normal decision = %v", normal.Decision)
	}
}

func validRuntimeIdentity() RuntimeIdentity {
	return RuntimeIdentity{
		Release: RuntimeRelease{
			SchemaVersion:         ContractSchemaVersion,
			ReleaseID:             "018f47d2-7f00-7abc-8def-1234567890ab",
			ReleaseManifestDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			ServiceVersion:        "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			WebAssetDigest:        "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		},
		Config: ConfigIdentity{
			SchemaVersion: ContractSchemaVersion,
			Revision:      "development-v1",
			Fingerprint:   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		Environment: "development",
		CellID:      "dev-a",
		RuntimeRole: "api",
	}
}

func validRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{
		CollectorEndpoint:      "http://collector.dev-a.accountable.internal:4318",
		Protocol:               TelemetryProtocol,
		SamplingPolicy:         TelemetrySamplingPolicy,
		RedactionPolicyVersion: TelemetryRedactionPolicy,
	}
}

func TestRuntimeConfigFailsClosed(t *testing.T) {
	if err := ValidateRuntimeConfig(validRuntimeConfig()); err != nil {
		t.Fatal(err)
	}
	rejected := map[string]func(*RuntimeConfig){
		"unknown protocol":       func(config *RuntimeConfig) { config.Protocol = "otlp_grpc" },
		"unknown sampling":       func(config *RuntimeConfig) { config.SamplingPolicy = "always_on" },
		"unknown redaction":      func(config *RuntimeConfig) { config.RedactionPolicyVersion = "allowlist_v2" },
		"missing endpoint":       func(config *RuntimeConfig) { config.CollectorEndpoint = "" },
		"credentialed endpoint":  func(config *RuntimeConfig) { config.CollectorEndpoint = "http://user:pass@collector:4318" },
		"wrong application port": func(config *RuntimeConfig) { config.CollectorEndpoint = "http://collector:4319" },
		"query in endpoint":      func(config *RuntimeConfig) { config.CollectorEndpoint = "http://collector:4318?a=b" },
	}
	for name, mutate := range rejected {
		t.Run(name, func(t *testing.T) {
			config := validRuntimeConfig()
			mutate(&config)
			if err := ValidateRuntimeConfig(config); !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestSLIEndpointComesFromTheValidatedCollectorHost(t *testing.T) {
	endpoint, err := SLIEndpoint(validRuntimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "http://collector.dev-a.accountable.internal:4319" {
		t.Fatalf("got %s", endpoint)
	}
}

func TestStartRuntimeWithoutTelemetryStaysQuiet(t *testing.T) {
	signals, err := StartRuntime(context.Background(), validRuntimeIdentity(), RuntimeConfig{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if signals.logger != nil || signals.providers != nil || signals.recorder != nil {
		t.Fatal("disabled telemetry must not attach stdout or providers")
	}
	signals.Emit(context.Background(), slog.LevelInfo, "runtime.task_completed", nil)
}

func TestProcessResourceKeepsIdentitiesSeparate(t *testing.T) {
	identity := validRuntimeIdentity()
	resource, err := BuildProcessResource(identity)
	if err != nil {
		t.Fatal(err)
	}
	if resource.ServiceName != "accountable-api" {
		t.Fatalf("service name = %s", resource.ServiceName)
	}
	if resource.ReleaseID == resource.ServiceVersion ||
		resource.ReleaseID == resource.ConfigurationRevision ||
		resource.ReleaseID == resource.ConfigurationFingerprint ||
		resource.ServiceVersion == resource.ConfigurationFingerprint {
		t.Fatal("release, artifact, and configuration identity must stay separate")
	}

	// A release identity that repeats the manifest digest is not an artifact identity.
	identity.Release.ServiceVersion = identity.Release.ReleaseManifestDigest
	if _, err := BuildProcessResource(identity); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("got %v", err)
	}
}

func TestRootJourneyAndCausationRules(t *testing.T) {
	root, err := StartRequestIdentity("")
	if err != nil {
		t.Fatal(err)
	}
	if root.RequestID != root.CorrelationID {
		t.Fatal("an absent inbound correlation ID must start a root journey")
	}
	if err := ValidateRootWorkIdentity(root); err != nil {
		t.Fatal(err)
	}

	invalid, err := StartRequestIdentity("not-a-uuid")
	if err != nil {
		t.Fatal(err)
	}
	if invalid.RequestID != invalid.CorrelationID {
		t.Fatal("an invalid inbound correlation ID must start a root journey")
	}

	inbound := "019261bb-4c10-7f3a-9aaa-0f1e2d3c4b5a"
	continued, err := StartRequestIdentity(inbound)
	if err != nil {
		t.Fatal(err)
	}
	if continued.CorrelationID != inbound || continued.RequestID == inbound {
		t.Fatal("a valid inbound correlation ID must continue its journey with a new request ID")
	}

	operation, err := WithOperation(continued, "")
	if err != nil {
		t.Fatal(err)
	}
	if operation.OperationID == operation.RequestID || operation.CausationID != "" {
		t.Fatal("an operation identity must be independent and must not invent causation")
	}
	if _, err := WithOperation(continued, "not-a-uuid"); !errors.Is(err, ErrInvalidContract) {
		t.Fatal("a fabricated causation identity must fail")
	}
}

func TestSafeLoggerRejectsUnregisteredEventsAndAttributes(t *testing.T) {
	var stdout bytes.Buffer
	resource, err := BuildProcessResource(validRuntimeIdentity())
	if err != nil {
		t.Fatal(err)
	}
	logger := NewSafeLogger(SafeLoggerOptions{Stdout: &stdout, Resource: resource})
	ctx := ContextWithWorkIdentity(context.Background(), WorkIdentity{
		RequestID:     "018f47d2-7f00-7abc-8def-1234567890ab",
		CorrelationID: "018f47d2-7f00-7abc-8def-1234567890ab",
	})
	if err := logger.Emit(ctx, slog.LevelInfo, "request.completed", map[string]string{
		"route_class": "home",
		"outcome":     "success",
	}); err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		StdoutFieldEventName, "service_name", "service_version", StdoutFieldEnvironment,
		StdoutFieldCellID, "accountable_release_id", "accountable_configuration_fingerprint",
		"request_id", "correlation_id",
	} {
		if _, present := record[name]; !present {
			t.Errorf("safe record is missing %s", name)
		}
	}
	if err := logger.Emit(ctx, slog.LevelInfo, "request.exploded", nil); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("unregistered event returned %v", err)
	}
	if err := logger.Emit(ctx, slog.LevelInfo, "request.completed", map[string]string{
		"authorization": "Bearer secret",
	}); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("unregistered attribute returned %v", err)
	}
}

func TestSafeLoggerRateLimitsIdentityFailure(t *testing.T) {
	var stdout bytes.Buffer
	fixed := clock.Fixed{Instant: time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)}
	logger := NewSafeLogger(SafeLoggerOptions{Stdout: &stdout, Clock: fixed})
	for range 5 {
		if err := logger.Emit(context.Background(), slog.LevelError, "identity.generation_failed", nil); err != nil {
			t.Fatal(err)
		}
	}
	records := strings.Count(strings.TrimSpace(stdout.String()), "\n") + 1
	if stdout.Len() == 0 {
		records = 0
	}
	if records != 1 {
		t.Fatalf("records = %d, want one for each process in one minute", records)
	}
}

// TestSafeStdoutFieldsMatchTheMetricFilters locks the exact JSON field names that
// the CloudWatch metric filters select. A dotted key is not addressable by a
// filter, so a rename here silently empties the loss and uncertainty counters.
func TestSafeStdoutFieldsMatchTheMetricFilters(t *testing.T) {
	var stdout bytes.Buffer
	resource, err := BuildProcessResource(validRuntimeIdentity())
	if err != nil {
		t.Fatal(err)
	}
	logger := NewSafeLogger(SafeLoggerOptions{Stdout: &stdout, Resource: resource})
	if err := logger.Emit(context.Background(), slog.LevelError, "telemetry.client_event_dropped", map[string]string{
		StdoutFieldReason:    "shutdown_timeout",
		StdoutFieldDropCount: "3",
	}); err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		StdoutFieldEventName, StdoutFieldCellID, StdoutFieldEnvironment,
		StdoutFieldReason, StdoutFieldDropCount,
	} {
		if _, present := record[field]; !present {
			t.Errorf("safe record is missing metric-filter field %q", field)
		}
	}
	for field := range record {
		if strings.Contains(field, ".") && field != "time" && field != "level" && field != "msg" {
			t.Errorf("field %q uses a dotted key that no metric filter can select", field)
		}
	}
	if record[StdoutFieldEventName] != "telemetry.client_event_dropped" {
		t.Fatalf("event name = %v", record[StdoutFieldEventName])
	}
}

func TestProvidersIsolatePublicTraceContext(t *testing.T) {
	providers, err := NewProviders(context.Background(), ProviderOptions{
		Identity: validRuntimeIdentity(),
		Config:   validRuntimeConfig(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = providers.Shutdown(shutdownContext)
	})
	recorder, err := NewRecorder(providers)
	if err != nil {
		t.Fatal(err)
	}
	publicTrace, err := oteltrace.TraceIDFromHex("11111111111111111111111111111111")
	if err != nil {
		t.Fatal(err)
	}
	publicSpan, err := oteltrace.SpanIDFromHex("2222222222222222")
	if err != nil {
		t.Fatal(err)
	}
	public := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    publicTrace,
		SpanID:     publicSpan,
		TraceFlags: oteltrace.FlagsSampled,
		Remote:     true,
	})
	_, span := recorder.StartServerSpan(context.Background(), "accountable.system.v1.SystemService/GetRuntime", public, nil)
	defer span.End()
	if span.SpanContext().TraceID() == publicTrace {
		t.Fatal("public trace context must be a link, never the server parent")
	}
}

func TestSpanAttributesRejectUnregisteredDescriptors(t *testing.T) {
	identity := WorkIdentity{
		RequestID:     "018f47d2-7f00-7abc-8def-1234567890ab",
		CorrelationID: "018f47d2-7f00-7abc-8def-1234567890ab",
	}
	attributes, err := SpanAttributes(identity, map[string]string{"route_class": "home", "outcome": "success"})
	if err != nil {
		t.Fatal(err)
	}
	if len(attributes) != 4 {
		t.Fatalf("attributes = %d", len(attributes))
	}
	if _, err := SpanAttributes(identity, map[string]string{"sql_text": "select 1"}); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("got %v", err)
	}
	if _, err := SpanAttributes(identity, map[string]string{"trace_id": "1"}); !errors.Is(err, ErrInvalidContract) {
		t.Fatal("native trace identity must not be duplicated as a span attribute")
	}
}

func TestRecorderRejectsUnregisteredMetricAttributes(t *testing.T) {
	providers, err := NewProviders(context.Background(), ProviderOptions{
		Identity: validRuntimeIdentity(),
		Config:   validRuntimeConfig(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = providers.Shutdown(shutdownContext)
	})
	recorder, err := NewRecorder(providers)
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.RequestFinished(context.Background(), ServerRequest{
		RouteClass: "home", RPCService: "accountable.system.v1.SystemService", RPCMethod: "GetRuntime",
		Outcome: "success", ErrorCategory: "none", Duration: 5 * time.Millisecond,
	}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.DatabaseFinished(context.Background(), DependencyOperation{
		Class: "read", Outcome: "success", ErrorCategory: "none", Duration: time.Millisecond,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProvidersShutdownIsBounded(t *testing.T) {
	providers, err := NewProviders(context.Background(), ProviderOptions{
		Identity: validRuntimeIdentity(),
		Config:   validRuntimeConfig(),
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 2*ProviderFlushTimeout)
	defer cancel()
	_ = providers.Shutdown(shutdownContext)
	if elapsed := time.Since(started); elapsed > 2*ProviderFlushTimeout {
		t.Fatalf("shutdown took %s", elapsed)
	}
}
