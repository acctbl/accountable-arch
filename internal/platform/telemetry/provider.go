package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otellog "go.opentelemetry.io/otel/log"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const (
	// SamplingRatio is the compiled 5-percent policy for normal server roots.
	SamplingRatio        = 0.05
	MetricExportInterval = 30 * time.Second
	// ProviderFlushTimeout is the total budget for flushing every provider.
	ProviderFlushTimeout = 5 * time.Second

	instrumentationScope = "github.com/acctbl/accountable/internal/platform/telemetry"
)

type Providers struct {
	resource        *sdkresource.Resource
	traces          *sdktrace.TracerProvider
	metrics         *sdkmetric.MeterProvider
	logs            *sdklog.LoggerProvider
	heartbeatCancel context.CancelFunc
	heartbeatDone   chan struct{}
}

// ProviderOptions carries no endpoint that configuration did not validate.
type ProviderOptions struct {
	Identity RuntimeIdentity
	Config   RuntimeConfig
}

// NewProviders keeps every exporter asynchronous: telemetry failure must never
// stop a user request, and every error surfaces through the safe stdout path.
func NewProviders(ctx context.Context, options ProviderOptions) (*Providers, error) {
	if err := ValidateRuntimeConfig(options.Config); err != nil {
		return nil, err
	}
	processResource, err := BuildProcessResource(options.Identity)
	if err != nil {
		return nil, err
	}
	attributes := resourceAttributes(processResource)
	sdkResource, err := sdkresource.Merge(
		sdkresource.Empty(),
		sdkresource.NewWithAttributes("", attributes...),
	)
	if err != nil {
		return nil, err
	}
	endpoint, secure, err := splitCollectorEndpoint(options.Config.CollectorEndpoint)
	if err != nil {
		return nil, err
	}
	sliEndpoint, err := SLIEndpoint(options.Config)
	if err != nil {
		return nil, err
	}
	sliHostPort, sliSecure, err := splitCollectorEndpoint(sliEndpoint)
	if err != nil {
		return nil, err
	}
	failureLogger := NewSafeLogger(SafeLoggerOptions{Stdout: os.Stdout, Resource: processResource})

	traceOptions := []otlptracehttp.Option{otlptracehttp.WithEndpoint(endpoint)}
	metricOptions := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(endpoint),
		otlpmetrichttp.WithTemporalitySelector(sdkmetric.CumulativeTemporalitySelector),
	}
	sliMetricOptions := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(sliHostPort),
		otlpmetrichttp.WithTemporalitySelector(sdkmetric.DeltaTemporalitySelector),
		otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{Enabled: false}),
	}
	logOptions := []otlploghttp.Option{otlploghttp.WithEndpoint(endpoint)}
	if !secure {
		traceOptions = append(traceOptions, otlptracehttp.WithInsecure())
		metricOptions = append(metricOptions, otlpmetrichttp.WithInsecure())
		logOptions = append(logOptions, otlploghttp.WithInsecure())
	}
	if !sliSecure {
		sliMetricOptions = append(sliMetricOptions, otlpmetrichttp.WithInsecure())
	}

	traceExporter, err := otlptracehttp.New(ctx, traceOptions...)
	if err != nil {
		return nil, err
	}
	metricExporter, err := otlpmetrichttp.New(ctx, metricOptions...)
	if err != nil {
		_ = traceExporter.Shutdown(context.Background())
		return nil, err
	}
	sliMetricExporter, err := otlpmetrichttp.New(ctx, sliMetricOptions...)
	if err != nil {
		_ = metricExporter.Shutdown(context.Background())
		_ = traceExporter.Shutdown(context.Background())
		return nil, err
	}
	logExporter, err := otlploghttp.New(ctx, logOptions...)
	if err != nil {
		_ = sliMetricExporter.Shutdown(context.Background())
		_ = metricExporter.Shutdown(context.Background())
		_ = traceExporter.Shutdown(context.Background())
		return nil, err
	}

	traces := sdktrace.NewTracerProvider(
		sdktrace.WithResource(sdkResource),
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithSampler(proofSampler{normal: sdktrace.ParentBased(sdktrace.TraceIDRatioBased(SamplingRatio))}),
	)
	metrics := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(sdkResource),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(
			&sliFailureExporter{Exporter: sliMetricExporter, logger: failureLogger},
			sdkmetric.WithInterval(MetricExportInterval),
		)),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(
			metricExporter,
			sdkmetric.WithInterval(MetricExportInterval),
		)),
		sdkmetric.WithView(instrumentViews()...),
	)
	logs := sdklog.NewLoggerProvider(
		sdklog.WithResource(sdkResource),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
	)
	heartbeat, err := metrics.Meter(instrumentationScope).Int64Counter("accountable.sli.export.heartbeat")
	if err != nil {
		_ = logs.Shutdown(context.Background())
		_ = metrics.Shutdown(context.Background())
		_ = traces.Shutdown(context.Background())
		return nil, err
	}
	heartbeatContext, heartbeatCancel := context.WithCancel(context.Background())
	heartbeatDone := make(chan struct{})
	heartbeat.Add(heartbeatContext, 1)
	go runSLIHeartbeat(heartbeatContext, heartbeatDone, heartbeat)
	return &Providers{
		resource:        sdkResource,
		traces:          traces,
		metrics:         metrics,
		logs:            logs,
		heartbeatCancel: heartbeatCancel,
		heartbeatDone:   heartbeatDone,
	}, nil
}

type proofSampler struct {
	normal sdktrace.Sampler
}

func (sampler proofSampler) ShouldSample(parameters sdktrace.SamplingParameters) sdktrace.SamplingResult {
	for _, value := range parameters.Attributes {
		if value.Key == "proof_type" && value.Value.AsString() == ProofType {
			return sdktrace.SamplingResult{Decision: sdktrace.RecordAndSample}
		}
	}
	return sampler.normal.ShouldSample(parameters)
}

func (sampler proofSampler) Description() string {
	return "AlwaysOnProofParentBasedTraceIDRatioBased"
}

type sliFailureExporter struct {
	sdkmetric.Exporter
	logger *SafeLogger
}

func (exporter *sliFailureExporter) Export(ctx context.Context, metrics *metricdata.ResourceMetrics) error {
	err := exporter.Exporter.Export(ctx, metrics)
	exporter.report(ctx, err)
	return err
}

func (exporter *sliFailureExporter) ForceFlush(ctx context.Context) error {
	err := exporter.Exporter.ForceFlush(ctx)
	exporter.report(ctx, err)
	return err
}

func (exporter *sliFailureExporter) Shutdown(ctx context.Context) error {
	err := exporter.Exporter.Shutdown(ctx)
	exporter.report(ctx, err)
	return err
}

func (exporter *sliFailureExporter) report(ctx context.Context, err error) {
	if err != nil {
		_ = exporter.logger.Emit(ctx, slog.LevelError, "telemetry.sli_export_failed", nil)
	}
}

func runSLIHeartbeat(ctx context.Context, done chan<- struct{}, heartbeat otelmetric.Int64Counter) {
	defer close(done)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			heartbeat.Add(ctx, 1)
		}
	}
}

func (providers *Providers) Tracer() oteltrace.Tracer {
	return providers.traces.Tracer(instrumentationScope)
}

func (providers *Providers) Meter() otelmetric.Meter {
	return providers.metrics.Meter(instrumentationScope)
}

func (providers *Providers) Logger() otellog.Logger {
	return providers.logs.Logger(instrumentationScope)
}

func (providers *Providers) LoggerProvider() otellog.LoggerProvider {
	return providers.logs
}

// BridgeHandler is the appender that carries application logs to the log
// provider. OpenTelemetry routes application logs through a bridge.
func (providers *Providers) BridgeHandler() slog.Handler {
	return otelslog.NewHandler(instrumentationScope, otelslog.WithLoggerProvider(providers.logs))
}

// Shutdown flushes inside one shared budget. The caller maps a failure to the
// safe stdout record and a nonzero exit status.
func (providers *Providers) Shutdown(ctx context.Context) error {
	shutdownContext, cancel := context.WithTimeout(ctx, ProviderFlushTimeout)
	defer cancel()
	if providers.heartbeatCancel != nil {
		providers.heartbeatCancel()
		<-providers.heartbeatDone
	}
	var failures []error
	if err := providers.metrics.Shutdown(shutdownContext); err != nil {
		failures = append(failures, fmt.Errorf("metric provider shutdown: %w", err))
	}
	if err := providers.traces.Shutdown(shutdownContext); err != nil {
		failures = append(failures, fmt.Errorf("trace provider shutdown: %w", err))
	}
	if err := providers.logs.Shutdown(shutdownContext); err != nil {
		failures = append(failures, fmt.Errorf("log provider shutdown: %w", err))
	}
	return errors.Join(failures...)
}

// instrumentViews discard every point attribute the metric registry does not
// permit, so application code cannot widen a series by mistake.
func instrumentViews() []sdkmetric.View {
	views := make([]sdkmetric.View, 0, len(registeredMetricAttributes)+1)
	for instrument, allowed := range registeredMetricAttributes {
		keys := make([]attribute.Key, 0, len(allowed))
		for name := range allowed {
			keys = append(keys, attribute.Key(name))
		}
		views = append(views, sdkmetric.NewView(
			sdkmetric.Instrument{Name: instrument, Scope: instrumentation.Scope{Name: instrumentationScope}},
			sdkmetric.Stream{AttributeFilter: attribute.NewAllowKeysFilter(keys...)},
		))
	}
	// An unregistered instrument never reaches an exporter.
	views = append(views, func(instrument sdkmetric.Instrument) (sdkmetric.Stream, bool) {
		if _, registered := registeredMetricAttributes[instrument.Name]; registered {
			return sdkmetric.Stream{}, false
		}
		return sdkmetric.Stream{Aggregation: sdkmetric.AggregationDrop{}}, true
	})
	return views
}

func resourceAttributes(resource ProcessResource) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("service.name", resource.ServiceName),
		attribute.String("service.version", resource.ServiceVersion),
		attribute.String("deployment.environment.name", resource.DeploymentEnvironmentName),
		attribute.String("accountable.cell.id", resource.CellID),
		attribute.String("accountable.runtime.role", resource.RuntimeRole),
		attribute.String("accountable.release.id", resource.ReleaseID),
		attribute.Int("accountable.configuration.schema_version", resource.ConfigurationSchemaVersion),
		attribute.String("accountable.configuration.revision", resource.ConfigurationRevision),
		attribute.String("accountable.configuration.fingerprint", resource.ConfigurationFingerprint),
		attribute.String("accountable.telemetry.protocol", resource.TelemetryProtocol),
		attribute.String("accountable.telemetry.sampling.policy", resource.TelemetrySamplingPolicy),
		attribute.String("accountable.telemetry.redaction.policy.version", resource.TelemetryRedactionPolicy),
	}
}

func splitCollectorEndpoint(endpoint string) (hostPort string, secure bool, err error) {
	parsed, parseErr := url.Parse(endpoint)
	if parseErr != nil || parsed.Host == "" {
		return "", false, contractError("collector_endpoint")
	}
	return parsed.Host, parsed.Scheme == "https", nil
}
