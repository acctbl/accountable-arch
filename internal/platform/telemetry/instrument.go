package telemetry

import (
	"context"
	"encoding/json"
	"math/big"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// Recorder owns every application instrument. Application code adds only the
// point attributes the metric registry permits.
type Recorder struct {
	tracer oteltrace.Tracer

	serverRequests   otelmetric.Int64Counter
	serverDuration   otelmetric.Int64Histogram
	serverActive     otelmetric.Int64UpDownCounter
	databaseCount    otelmetric.Int64Counter
	databaseDuration otelmetric.Int64Histogram
	sliEligible      otelmetric.Int64Counter
	sliBad           otelmetric.Int64Counter
	sliSlow          otelmetric.Int64Counter
	clientEvents     otelmetric.Int64Counter
	clientDuration   otelmetric.Int64Histogram
	webVitalDuration otelmetric.Int64Histogram
	webVitalCLS      otelmetric.Int64Histogram
	proofEvents      otelmetric.Int64Counter
}

type ServerRequest struct {
	RouteClass    string
	RPCService    string
	RPCMethod     string
	Outcome       string
	ErrorCategory string
	Duration      time.Duration
	Eligible      bool
}

type DependencyOperation struct {
	Class         string
	Outcome       string
	ErrorCategory string
	Duration      time.Duration
}

func NewRecorder(providers *Providers) (*Recorder, error) {
	meter := providers.Meter()
	recorder := &Recorder{tracer: providers.Tracer()}
	var err error
	if recorder.serverRequests, err = meter.Int64Counter("accountable.server.request.count"); err != nil {
		return nil, err
	}
	if recorder.serverDuration, err = meter.Int64Histogram(
		"accountable.server.request.duration",
		otelmetric.WithUnit("ms"),
	); err != nil {
		return nil, err
	}
	if recorder.serverActive, err = meter.Int64UpDownCounter("accountable.server.request.active"); err != nil {
		return nil, err
	}
	if recorder.databaseCount, err = meter.Int64Counter("accountable.db.operation.count"); err != nil {
		return nil, err
	}
	if recorder.databaseDuration, err = meter.Int64Histogram(
		"accountable.db.operation.duration",
		otelmetric.WithUnit("ms"),
	); err != nil {
		return nil, err
	}
	if recorder.sliEligible, err = meter.Int64Counter("accountable.sli.request.eligible"); err != nil {
		return nil, err
	}
	if recorder.sliBad, err = meter.Int64Counter("accountable.sli.availability.bad"); err != nil {
		return nil, err
	}
	if recorder.sliSlow, err = meter.Int64Counter("accountable.sli.latency.slow"); err != nil {
		return nil, err
	}
	if recorder.clientEvents, err = meter.Int64Counter("accountable.client.event.count"); err != nil {
		return nil, err
	}
	if recorder.clientDuration, err = meter.Int64Histogram("accountable.client.duration", otelmetric.WithUnit("ms")); err != nil {
		return nil, err
	}
	if recorder.webVitalDuration, err = meter.Int64Histogram("accountable.browser.web_vital.duration_us", otelmetric.WithUnit("us")); err != nil {
		return nil, err
	}
	if recorder.webVitalCLS, err = meter.Int64Histogram("accountable.browser.web_vital.cls_ppm"); err != nil {
		return nil, err
	}
	if recorder.proofEvents, err = meter.Int64Counter("accountable.observability.proof"); err != nil {
		return nil, err
	}
	return recorder, nil
}

func (recorder *Recorder) ProofProcessed(ctx context.Context) {
	recorder.proofEvents.Add(ctx, 1, otelmetric.WithAttributes(attribute.String("proof_type", ProofType)))
}

func (recorder *Recorder) ClientEventProcessed(ctx context.Context, envelope ClientEnvelope) error {
	identity := []attribute.KeyValue{
		attribute.String("client_application", envelope.ClientApplication),
		attribute.String("release_id", envelope.ReleaseID),
		attribute.String("client_artifact_type", envelope.ClientArtifactType),
		attribute.String("client_artifact_digest", envelope.ClientArtifactDigest),
		attribute.Int("configuration_schema_version", envelope.ConfigurationSchemaVersion),
		attribute.String("configuration_revision", envelope.ConfigurationRevision),
		attribute.String("configuration_fingerprint", envelope.ConfigurationFingerprint),
	}
	outcome := stringAttribute(envelope.Attributes, "outcome", "success")
	errorCategory := stringAttribute(envelope.Attributes, "error_category", "none")
	counterAttributes := append([]attribute.KeyValue{
		attribute.String("event_name", envelope.EventName),
		attribute.String("route_class", envelope.RouteClass),
		attribute.String("outcome", outcome),
		attribute.String("error_category", errorCategory),
	}, identity...)
	recorder.clientEvents.Add(ctx, 1, otelmetric.WithAttributes(counterAttributes...))
	if duration, ok := integerAttribute(envelope.Attributes, "duration_ms"); ok {
		recorder.clientDuration.Record(ctx, duration, otelmetric.WithAttributes(append([]attribute.KeyValue{
			attribute.String("event_name", envelope.EventName),
			attribute.String("route_class", envelope.RouteClass),
			attribute.String("outcome", outcome),
		}, identity...)...))
	}
	if value, ok := scaledNumberAttribute(envelope.Attributes, "value", 1000); ok {
		if envelope.EventName == "web_vital.cls" {
			cls, valid := scaledNumberAttribute(envelope.Attributes, "value", 1000000)
			if !valid {
				return ErrInvalidContract
			}
			recorder.webVitalCLS.Record(ctx, cls, otelmetric.WithAttributes(append([]attribute.KeyValue{
				attribute.String("route_class", envelope.RouteClass),
			}, identity...)...))
		} else {
			recorder.webVitalDuration.Record(ctx, value, otelmetric.WithAttributes(append([]attribute.KeyValue{
				attribute.String("event_name", envelope.EventName),
				attribute.String("route_class", envelope.RouteClass),
			}, identity...)...))
		}
	}
	return nil
}

func stringAttribute(attributes map[string]any, name, fallback string) string {
	if value, ok := attributes[name].(string); ok {
		return value
	}
	return fallback
}

func integerAttribute(attributes map[string]any, name string) (int64, bool) {
	number, ok := attributes[name].(json.Number)
	if !ok {
		return 0, false
	}
	value, err := strconv.ParseInt(number.String(), 10, 64)
	return value, err == nil
}

func scaledNumberAttribute(attributes map[string]any, name string, scale int64) (int64, bool) {
	number, ok := attributes[name].(json.Number)
	if !ok {
		return 0, false
	}
	value, valid := new(big.Rat).SetString(number.String())
	if !valid || value.Sign() < 0 {
		return 0, false
	}
	value.Mul(value, big.NewRat(scale, 1))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(value.Num(), value.Denom(), remainder)
	if new(big.Int).Mul(remainder, big.NewInt(2)).Cmp(value.Denom()) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, false
	}
	return quotient.Int64(), true
}

// StartServerSpan starts one server span. Public trace context is a link only, so
// a caller can never control the server parent or the server sample.
func (recorder *Recorder) StartServerSpan(
	ctx context.Context,
	name string,
	publicContext oteltrace.SpanContext,
	attributes []attribute.KeyValue,
) (context.Context, oteltrace.Span) {
	options := []oteltrace.SpanStartOption{
		oteltrace.WithSpanKind(oteltrace.SpanKindServer),
		oteltrace.WithAttributes(attributes...),
	}
	if publicContext.IsValid() {
		options = append(options, oteltrace.WithLinks(oteltrace.Link{SpanContext: publicContext}))
	}
	return recorder.tracer.Start(oteltrace.ContextWithSpanContext(ctx, oteltrace.SpanContext{}), name, options...)
}

func (recorder *Recorder) StartInternalSpan(
	ctx context.Context,
	name string,
	kind oteltrace.SpanKind,
	attributes []attribute.KeyValue,
) (context.Context, oteltrace.Span) {
	return recorder.tracer.Start(ctx, name,
		oteltrace.WithSpanKind(kind),
		oteltrace.WithAttributes(attributes...),
	)
}

func SpanAttributes(identity WorkIdentity, descriptors map[string]string) ([]attribute.KeyValue, error) {
	allowed, err := RegisteredSignalAttributes("trace")
	if err != nil {
		return nil, err
	}
	attributes := make([]attribute.KeyValue, 0, len(descriptors)+6)
	for name, value := range WorkIdentityAttributes(identity) {
		attributes = append(attributes, attribute.String(name, value))
	}
	for name, value := range descriptors {
		if !contains(allowed, name) {
			return nil, contractError("span attribute")
		}
		attributes = append(attributes, attribute.String(name, value))
	}
	return attributes, nil
}

func (recorder *Recorder) RequestStarted(ctx context.Context, routeClass string) {
	recorder.serverActive.Add(ctx, 1, otelmetric.WithAttributes(attribute.String("route_class", routeClass)))
}

func (recorder *Recorder) RequestFinished(ctx context.Context, request ServerRequest) error {
	if err := ValidateMetricAttributes("accountable.server.request.count", map[string]any{
		"route_class":    request.RouteClass,
		"rpc_service":    request.RPCService,
		"rpc_method":     request.RPCMethod,
		"outcome":        request.Outcome,
		"error_category": request.ErrorCategory,
	}); err != nil {
		return err
	}
	recorder.serverActive.Add(ctx, -1, otelmetric.WithAttributes(attribute.String("route_class", request.RouteClass)))
	recorder.serverRequests.Add(ctx, 1, otelmetric.WithAttributes(
		attribute.String("route_class", request.RouteClass),
		attribute.String("rpc_service", request.RPCService),
		attribute.String("rpc_method", request.RPCMethod),
		attribute.String("outcome", request.Outcome),
		attribute.String("error_category", request.ErrorCategory),
	))
	recorder.serverDuration.Record(ctx, request.Duration.Milliseconds(), otelmetric.WithAttributes(
		attribute.String("route_class", request.RouteClass),
		attribute.String("rpc_service", request.RPCService),
		attribute.String("rpc_method", request.RPCMethod),
		attribute.String("outcome", request.Outcome),
	))
	if request.Eligible {
		recorder.sliEligible.Add(ctx, 1)
		if request.Outcome == "failure" {
			recorder.sliBad.Add(ctx, 1)
		}
		if request.Duration > time.Second {
			recorder.sliSlow.Add(ctx, 1)
		}
	}
	return nil
}

func (recorder *Recorder) DatabaseFinished(ctx context.Context, operation DependencyOperation) error {
	if err := ValidateMetricAttributes("accountable.db.operation.count", map[string]any{
		"operation_class": operation.Class,
		"outcome":         operation.Outcome,
		"error_category":  operation.ErrorCategory,
	}); err != nil {
		return err
	}
	recorder.databaseCount.Add(ctx, 1, otelmetric.WithAttributes(
		attribute.String("operation_class", operation.Class),
		attribute.String("outcome", operation.Outcome),
		attribute.String("error_category", operation.ErrorCategory),
	))
	recorder.databaseDuration.Record(ctx, operation.Duration.Milliseconds(), otelmetric.WithAttributes(
		attribute.String("operation_class", operation.Class),
		attribute.String("outcome", operation.Outcome),
	))
	return nil
}
