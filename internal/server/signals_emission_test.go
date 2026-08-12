package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/acctbl/accountable/internal/platform/telemetry"
	"github.com/acctbl/accountable/internal/server"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const publicTraceHex = "11111111111111111111111111111111"

// collectorRecorder stands in for the cell Collector and keeps what each signal
// path received.
type collectorRecorder struct {
	mutex   sync.Mutex
	paths   map[string]int
	traces  []ptrace.Traces
	metrics map[string][]pmetric.Metrics
}

func (recorder *collectorRecorder) handler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		recorder.mutex.Lock()
		defer recorder.mutex.Unlock()
		recorder.paths[request.URL.Path]++
		if request.URL.Path == "/v1/traces" {
			export := ptraceotlp.NewExportRequest()
			if err := export.UnmarshalProto(body); err == nil {
				recorder.traces = append(recorder.traces, export.Traces())
			}
		}
		if request.URL.Path == "/v1/metrics" {
			export := pmetricotlp.NewExportRequest()
			if err := export.UnmarshalProto(body); err == nil {
				recorder.metrics[request.Host] = append(recorder.metrics[request.Host], export.Metrics())
			}
		}
		writer.WriteHeader(http.StatusOK)
	})
}

func (recorder *collectorRecorder) hasMetric(host, name string, temporality pmetric.AggregationTemporality) bool {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	for _, batch := range recorder.metrics[host] {
		for resourceIndex := 0; resourceIndex < batch.ResourceMetrics().Len(); resourceIndex++ {
			scopes := batch.ResourceMetrics().At(resourceIndex).ScopeMetrics()
			for scopeIndex := 0; scopeIndex < scopes.Len(); scopeIndex++ {
				metrics := scopes.At(scopeIndex).Metrics()
				for metricIndex := 0; metricIndex < metrics.Len(); metricIndex++ {
					metric := metrics.At(metricIndex)
					if metric.Name() == name && metric.Type() == pmetric.MetricTypeSum &&
						metric.Sum().AggregationTemporality() == temporality && metric.Sum().IsMonotonic() &&
						metric.Sum().DataPoints().At(0).Attributes().Len() == 0 {
						return true
					}
				}
			}
		}
	}
	return false
}

func (recorder *collectorRecorder) received(path string) int {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	return recorder.paths[path]
}

// TestSignalInterceptorEmitsEverySignal proves every signal path reaches the
// Collector, that the public traceparent arrives only as a link, and that no
// span carries an attribute outside the registry.
func TestSignalInterceptorEmitsEverySignal(t *testing.T) {
	recorder := &collectorRecorder{paths: make(map[string]int), metrics: make(map[string][]pmetric.Metrics)}
	listener, err := net.Listen("tcp", "127.0.0.1:4318")
	if err != nil {
		t.Skipf("the Collector application port is already in use: %v", err)
	}
	collector := &httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: recorder.handler(), ReadHeaderTimeout: time.Second},
	}
	collector.Start()
	t.Cleanup(collector.Close)
	sliListener, err := net.Listen("tcp", "127.0.0.1:4319")
	if err != nil {
		t.Skipf("the Collector SLI port is already in use: %v", err)
	}
	sliCollector := &httptest.Server{
		Listener: sliListener,
		Config:   &http.Server{Handler: recorder.handler(), ReadHeaderTimeout: time.Second},
	}
	sliCollector.Start()
	t.Cleanup(sliCollector.Close)

	identity := telemetry.RuntimeIdentity{
		Release: telemetry.RuntimeRelease{
			SchemaVersion:         1,
			ReleaseID:             "018f47d2-7f00-7abc-8def-1234567890ab",
			ReleaseManifestDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			ServiceVersion:        "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			WebAssetDigest:        "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		},
		Config: telemetry.ConfigIdentity{
			SchemaVersion: 1,
			Revision:      "signal-proof",
			Fingerprint:   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		Environment: "development",
		CellID:      "dev-a",
		RuntimeRole: "api",
	}
	providers, err := telemetry.NewProviders(context.Background(), telemetry.ProviderOptions{
		Identity: identity,
		Config: telemetry.RuntimeConfig{
			CollectorEndpoint:      collector.URL,
			Protocol:               telemetry.TelemetryProtocol,
			SamplingPolicy:         telemetry.TelemetrySamplingPolicy,
			RedactionPolicyVersion: telemetry.TelemetryRedactionPolicy,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	resource, err := telemetry.BuildProcessResource(identity)
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	logger := telemetry.NewSafeLogger(telemetry.SafeLoggerOptions{
		Stdout:   &stdout,
		Resource: resource,
		Bridge:   slog.New(providers.BridgeHandler()),
	})
	signals, err := telemetry.NewRecorder(providers)
	if err != nil {
		t.Fatal(err)
	}

	interceptor := server.SignalInterceptor{Recorder: signals, Logger: logger}
	handler := interceptor.WrapUnary(func(_ context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&struct{}{}), nil
	})

	// The compiled policy samples 5 percent of roots, so many requests are needed
	// before a sampled span is certain.
	const requests = 300
	for range requests {
		work, startErr := telemetry.StartRequestIdentity("")
		if startErr != nil {
			t.Fatal(startErr)
		}
		ctx := telemetry.ContextWithWorkIdentity(context.Background(), work)
		request := connect.NewRequest(&struct{}{})
		request.Header().Set("traceparent", "00-"+publicTraceHex+"-2222222222222222-01")
		request.Header().Set("tracestate", "vendor=value")
		if _, callErr := handler(ctx, request); callErr != nil {
			t.Fatal(callErr)
		}
	}
	if err := signals.RequestFinished(context.Background(), telemetry.ServerRequest{
		RouteClass:    "system",
		RPCService:    "accountable.system.v1.SystemService",
		RPCMethod:     "GetRuntime",
		Outcome:       "failure",
		ErrorCategory: "internal_failure",
		Duration:      1100 * time.Millisecond,
		Eligible:      true,
	}); err != nil {
		t.Fatal(err)
	}

	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := providers.Shutdown(shutdown); err != nil {
		t.Fatalf("provider shutdown: %v", err)
	}

	for _, path := range []string{"/v1/traces", "/v1/metrics", "/v1/logs"} {
		if recorder.received(path) == 0 {
			t.Errorf("nothing reached %s", path)
		}
	}
	for _, name := range []string{
		"accountable.sli.request.eligible",
		"accountable.sli.availability.bad",
		"accountable.sli.latency.slow",
		"accountable.sli.export.heartbeat",
	} {
		if !recorder.hasMetric("127.0.0.1:4319", name, pmetric.AggregationTemporalityDelta) {
			t.Errorf("delta SLI metric %s did not reach port 4319 without attributes", name)
		}
	}

	recorder.mutex.Lock()
	collected := append([]ptrace.Traces(nil), recorder.traces...)
	recorder.mutex.Unlock()
	assertServerSpansAreIsolated(t, collected)

	var record map[string]any
	if err := json.Unmarshal(bytes.SplitN(stdout.Bytes(), []byte("\n"), 2)[0], &record); err != nil {
		t.Fatalf("safe record: %v", err)
	}
	if record[telemetry.StdoutFieldEventName] != "request.completed" {
		t.Fatalf("event = %v", record[telemetry.StdoutFieldEventName])
	}
	for _, field := range []string{"request_id", "correlation_id", "operation_id", "route_class", "outcome"} {
		if _, present := record[field]; !present {
			t.Errorf("safe record is missing %s", field)
		}
	}
}

// assertServerSpansAreIsolated proves the public trace context stayed a link and
// that no span carries an attribute outside the registry.
func assertServerSpansAreIsolated(t *testing.T, collected []ptrace.Traces) {
	t.Helper()

	allowed, err := telemetry.RegisteredSignalAttributes("trace")
	if err != nil {
		t.Fatal(err)
	}
	permitted := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		permitted[name] = struct{}{}
	}
	publicTrace, err := oteltrace.TraceIDFromHex(publicTraceHex)
	if err != nil {
		t.Fatal(err)
	}

	spans := 0
	for _, span := range serverSpans(collected) {
		spans++
		if span.TraceID() == pcommon.TraceID(publicTrace) {
			t.Fatal("a public trace ID became the server trace")
		}
		if span.Links().Len() != 1 || span.Links().At(0).TraceID() != pcommon.TraceID(publicTrace) {
			t.Fatal("the public trace context is not carried as exactly one link")
		}
		span.Attributes().Range(func(name string, _ pcommon.Value) bool {
			if _, ok := permitted[name]; !ok {
				t.Errorf("span carries unregistered attribute %q", name)
			}
			return true
		})
	}
	if spans == 0 {
		t.Fatal("no span reached the Collector")
	}
}

func serverSpans(collected []ptrace.Traces) []ptrace.Span {
	var spans []ptrace.Span
	for _, traces := range collected {
		for resourceIndex := range traces.ResourceSpans().Len() {
			scopeSpans := traces.ResourceSpans().At(resourceIndex).ScopeSpans()
			for scopeIndex := range scopeSpans.Len() {
				spanSlice := scopeSpans.At(scopeIndex).Spans()
				for spanIndex := range spanSlice.Len() {
					spans = append(spans, spanSlice.At(spanIndex))
				}
			}
		}
	}
	return spans
}
