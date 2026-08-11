package telemetry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
)

type deliveryCounterReader interface {
	DeliveryCounters() DeliveryCounters
}

func TestAccountableDeliveryExportsAcceptedSpans(t *testing.T) {
	response, err := ptraceotlp.NewExportResponse().MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan ptrace.Traces, 1)
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/traces" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if request.Header.Get("Content-Type") != "application/x-protobuf" {
			t.Errorf("content type = %q", request.Header.Get("Content-Type"))
		}
		body := ptraceotlp.NewExportRequest()
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		if err := body.UnmarshalProto(raw); err != nil {
			t.Errorf("decode request: %v", err)
		}
		received <- body.Traces()
		writer.Header().Set("Content-Type", "application/x-protobuf")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(response)
	}))
	t.Cleanup(backend.Close)

	created := createTestTracesExporter(t, backend.URL, 10, testRetryConfig())
	if err := created.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	traces := tracesWithSpanCount(3)
	if err := created.ConsumeTraces(context.Background(), traces); err != nil {
		t.Fatal(err)
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := created.Shutdown(shutdownContext); err != nil {
		t.Fatal(err)
	}
	if got := (<-received).SpanCount(); got != 3 {
		t.Fatalf("received %d spans", got)
	}
	assertDeliveryCounters(t, created, DeliveryCounters{AcceptedItemCount: 3, SentItemCount: 3})
}

func TestAccountableDeliveryRetriesBeforeRecordingSuccess(t *testing.T) {
	response, err := ptraceotlp.NewExportResponse().MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	var attempts atomic.Int64
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(response)
	}))
	t.Cleanup(backend.Close)

	created := createTestTracesExporter(t, backend.URL, 10, testRetryConfig())
	if err := created.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if err := created.ConsumeTraces(context.Background(), tracesWithSpanCount(2)); err != nil {
		t.Fatal(err)
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := created.Shutdown(shutdownContext); err != nil {
		t.Fatal(err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts = %d", got)
	}
	assertDeliveryCounters(t, created, DeliveryCounters{AcceptedItemCount: 2, SentItemCount: 2})
}

func TestAccountableDeliveryRecordsPartialSuccess(t *testing.T) {
	response := ptraceotlp.NewExportResponse()
	response.PartialSuccess().SetRejectedSpans(2)
	rawResponse, err := response.MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(rawResponse)
	}))
	t.Cleanup(backend.Close)

	created := createTestTracesExporter(t, backend.URL, 10, testRetryConfig())
	if err := created.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if err := created.ConsumeTraces(context.Background(), tracesWithSpanCount(5)); err != nil {
		t.Fatal(err)
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := created.Shutdown(shutdownContext); err != nil {
		t.Fatal(err)
	}
	assertDeliveryCounters(t, created, DeliveryCounters{
		AcceptedItemCount:           5,
		SentItemCount:               3,
		PermanentlyDroppedItemCount: 2,
	})
}

func TestAccountableDeliveryRecordsFinalRetryFailure(t *testing.T) {
	var attempts atomic.Int64
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(backend.Close)

	created := createTestTracesExporter(t, backend.URL, 10, testRetryConfig())
	if err := created.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if err := created.ConsumeTraces(context.Background(), tracesWithSpanCount(4)); err != nil {
		t.Fatal(err)
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := created.Shutdown(shutdownContext); err != nil {
		t.Fatal(err)
	}
	if got := attempts.Load(); got < 2 {
		t.Fatalf("attempts = %d", got)
	}
	assertDeliveryCounters(t, created, DeliveryCounters{
		AcceptedItemCount:           4,
		PermanentlyDroppedItemCount: 4,
	})
}

func TestAccountableDeliveryRefusesQueueOverflowWithoutAcceptingIt(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		select {
		case <-requestStarted:
		default:
			close(requestStarted)
		}
		<-releaseRequest
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(backend.Close)

	created := createTestTracesExporter(t, backend.URL, 2, testRetryConfig())
	if err := created.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if err := created.ConsumeTraces(context.Background(), tracesWithSpanCount(1)); err != nil {
		t.Fatal(err)
	}
	<-requestStarted
	if err := created.ConsumeTraces(context.Background(), tracesWithSpanCount(2)); err != nil {
		t.Fatal(err)
	}
	if err := created.ConsumeTraces(context.Background(), tracesWithSpanCount(1)); err == nil {
		t.Fatal("queue overflow was accepted")
	}
	assertDeliveryCounters(t, created, DeliveryCounters{AcceptedItemCount: 3, PendingItemCount: 3})
	close(releaseRequest)
	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := created.Shutdown(shutdownContext); err != nil {
		t.Fatal(err)
	}
	assertDeliveryCounters(t, created, DeliveryCounters{AcceptedItemCount: 3, SentItemCount: 3})
}

func TestAccountableDeliveryShutdownLeavesUnconfirmedItemsPending(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	defer close(releaseRequest)
	backend := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		select {
		case <-request.Context().Done():
		case <-releaseRequest:
		}
	}))
	t.Cleanup(backend.Close)

	retry := testRetryConfig()
	retry.MaxElapsedTime = 2 * time.Second
	created := createTestTracesExporter(t, backend.URL, 10, retry)
	if err := created.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if err := created.ConsumeTraces(context.Background(), tracesWithSpanCount(2)); err != nil {
		t.Fatal(err)
	}
	<-requestStarted
	time.Sleep(1100 * time.Millisecond)
	pending := created.(deliveryCounterReader).DeliveryCounters()
	if pending.AcceptedItemCount != 2 || pending.PendingItemCount != 2 || pending.OldestPendingItemAgeSeconds < 1 {
		t.Fatalf("pending counters = %+v", pending)
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := created.Shutdown(shutdownContext); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown error = %v", err)
	}
	pending = created.(deliveryCounterReader).DeliveryCounters()
	if pending.AcceptedItemCount != 2 || pending.PendingItemCount != 2 || pending.OldestPendingItemAgeSeconds < 1 {
		t.Fatalf("pending counters after shutdown = %+v", pending)
	}
}

func TestAccountableDeliveryCrashCannotInventTerminalOutcomes(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		<-releaseRequest
	}))
	t.Cleanup(backend.Close)

	created := createTestTracesExporter(t, backend.URL, 10, testRetryConfig())
	if err := created.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if err := created.ConsumeTraces(context.Background(), tracesWithSpanCount(2)); err != nil {
		t.Fatal(err)
	}
	<-requestStarted
	created.(*accountableTracesExporter).delivery.cancel()
	close(releaseRequest)
	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := created.Shutdown(shutdownContext); err == nil {
		t.Fatal("unconfirmed crash state allowed a clean shutdown")
	}
	assertDeliveryCounters(t, created, DeliveryCounters{AcceptedItemCount: 2, PendingItemCount: 2})
}

func TestAccountableDeliveryExportsMetricsAndLogs(t *testing.T) {
	receivedMetrics := make(chan int, 1)
	receivedLogs := make(chan int, 1)
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		switch request.URL.Path {
		case "/v1/metrics":
			body := pmetricotlp.NewExportRequest()
			if err := body.UnmarshalProto(raw); err != nil {
				t.Errorf("decode metrics: %v", err)
			}
			receivedMetrics <- body.Metrics().DataPointCount()
			response, _ := pmetricotlp.NewExportResponse().MarshalProto()
			_, _ = writer.Write(response)
		case "/v1/logs":
			body := plogotlp.NewExportRequest()
			if err := body.UnmarshalProto(raw); err != nil {
				t.Errorf("decode logs: %v", err)
			}
			receivedLogs <- body.Logs().LogRecordCount()
			response, _ := plogotlp.NewExportResponse().MarshalProto()
			_, _ = writer.Write(response)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(backend.Close)

	factory := NewAccountableDeliveryFactory()
	metricsConfig := testDeliveryConfig(factory, backend.URL, 10, testRetryConfig())
	metricsExporter, err := factory.CreateMetrics(context.Background(), testExporterSettings(factory), metricsConfig)
	if err != nil {
		t.Fatal(err)
	}
	logsConfig := testDeliveryConfig(factory, backend.URL, 10, testRetryConfig())
	logsExporter, err := factory.CreateLogs(context.Background(), testExporterSettings(factory), logsConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := metricsExporter.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if err := logsExporter.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if err := metricsExporter.ConsumeMetrics(context.Background(), metricsWithDataPointCount(2)); err != nil {
		t.Fatal(err)
	}
	if err := logsExporter.ConsumeLogs(context.Background(), logsWithRecordCount(3)); err != nil {
		t.Fatal(err)
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := metricsExporter.Shutdown(shutdownContext); err != nil {
		t.Fatal(err)
	}
	if err := logsExporter.Shutdown(shutdownContext); err != nil {
		t.Fatal(err)
	}
	if got := <-receivedMetrics; got != 2 {
		t.Fatalf("received %d metric data points", got)
	}
	if got := <-receivedLogs; got != 3 {
		t.Fatalf("received %d log records", got)
	}
	assertDeliveryCounters(t, metricsExporter, DeliveryCounters{AcceptedItemCount: 2, SentItemCount: 2})
	assertDeliveryCounters(t, logsExporter, DeliveryCounters{AcceptedItemCount: 3, SentItemCount: 3})
}

func createTestTracesExporter(t *testing.T, endpoint string, queueSize int, retry configretry.BackOffConfig) exporter.Traces {
	t.Helper()
	factory := NewAccountableDeliveryFactory()
	config := testDeliveryConfig(factory, endpoint, queueSize, retry)
	created, err := factory.CreateTraces(context.Background(), testExporterSettings(factory), config)
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func testDeliveryConfig(factory exporter.Factory, endpoint string, queueSize int, retry configretry.BackOffConfig) *AccountableDeliveryConfig {
	config := factory.CreateDefaultConfig().(*AccountableDeliveryConfig)
	config.Endpoint = endpoint
	config.Environment = "staging"
	config.CellID = "stg-a"
	config.CollectorTaskID = "0123456789abcdef0123456789abcdef"
	config.SendingQueue.QueueSize = int64(queueSize)
	config.SendingQueue.NumConsumers = 1
	config.RetryOnFailure = retry
	return config
}

func testExporterSettings(factory exporter.Factory) exporter.Settings {
	return exporter.Settings{
		ID: component.NewID(factory.Type()),
	}
}

func testRetryConfig() configretry.BackOffConfig {
	config := configretry.NewDefaultBackOffConfig()
	config.InitialInterval = time.Millisecond
	config.RandomizationFactor = 0
	config.MaxInterval = time.Millisecond
	config.MaxElapsedTime = 25 * time.Millisecond
	return config
}

func tracesWithSpanCount(count int) ptrace.Traces {
	traces := ptrace.NewTraces()
	spans := traces.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans()
	for range count {
		spans.AppendEmpty()
	}
	return traces
}

func metricsWithDataPointCount(count int) pmetric.Metrics {
	metrics := pmetric.NewMetrics()
	dataPoints := metrics.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty().SetEmptyGauge().DataPoints()
	for range count {
		dataPoints.AppendEmpty()
	}
	return metrics
}

func logsWithRecordCount(count int) plog.Logs {
	logs := plog.NewLogs()
	records := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords()
	for range count {
		records.AppendEmpty()
	}
	return logs
}

func assertDeliveryCounters(t *testing.T, exporterComponent any, want DeliveryCounters) {
	t.Helper()
	got := exporterComponent.(deliveryCounterReader).DeliveryCounters()
	if got != want {
		t.Fatalf("delivery counters = %+v, want %+v", got, want)
	}
}
