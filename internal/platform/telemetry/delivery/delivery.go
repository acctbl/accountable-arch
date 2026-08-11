package telemetry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/acctbl/accountable/internal/platform/clock"
	"github.com/cenkalti/backoff/v7"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var accountableDeliveryType = component.MustNewType("accountable_delivery")

type AccountableDeliveryQueueConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
	NumConsumers int    `mapstructure:"num_consumers"`
	Sizer        string `mapstructure:"sizer"`
	QueueSize    int64  `mapstructure:"queue_size"`
}

type AccountableDeliveryConfig struct {
	confighttp.ClientConfig `mapstructure:",squash"`
	Environment             string                         `mapstructure:"environment"`
	CellID                  string                         `mapstructure:"cell_id"`
	CollectorTaskID         string                         `mapstructure:"collector_task_id"`
	RetryOnFailure          configretry.BackOffConfig      `mapstructure:"retry_on_failure"`
	SendingQueue            AccountableDeliveryQueueConfig `mapstructure:"sending_queue"`
}

func (config *AccountableDeliveryConfig) Validate() error {
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return contractError("accountable_delivery endpoint")
	}
	if endpoint.Scheme != "https" && (endpoint.Scheme != "http" || endpoint.Hostname() != "127.0.0.1") {
		return contractError("accountable_delivery endpoint")
	}
	if !contains(registeredEnvironments, config.Environment) || !cellIDPattern.MatchString(config.CellID) ||
		!collectorTaskIDPattern.MatchString(config.CollectorTaskID) {
		return contractError("accountable_delivery identity")
	}
	if !config.SendingQueue.Enabled || config.SendingQueue.NumConsumers < 1 ||
		config.SendingQueue.Sizer != "items" || config.SendingQueue.QueueSize < 1 {
		return contractError("accountable_delivery queue")
	}
	if !config.RetryOnFailure.Enabled || config.RetryOnFailure.MaxElapsedTime <= 0 ||
		config.RetryOnFailure.MaxElapsedTime > 60*time.Second {
		return contractError("accountable_delivery retry")
	}
	if err := config.RetryOnFailure.Validate(); err != nil {
		return fmt.Errorf("%w: accountable_delivery retry: %v", ErrInvalidContract, err)
	}
	if err := config.ClientConfig.Validate(); err != nil {
		return fmt.Errorf("%w: accountable_delivery HTTP client: %v", ErrInvalidContract, err)
	}
	return nil
}

func NewAccountableDeliveryFactory() exporter.Factory {
	return exporter.NewFactory(
		accountableDeliveryType,
		newAccountableDeliveryConfig,
		exporter.WithTraces(createAccountableTracesExporter, component.StabilityLevelAlpha),
		exporter.WithMetrics(createAccountableMetricsExporter, component.StabilityLevelAlpha),
		exporter.WithLogs(createAccountableLogsExporter, component.StabilityLevelAlpha),
	)
}

func newAccountableDeliveryConfig() component.Config {
	httpConfig := confighttp.NewDefaultClientConfig()
	httpConfig.Timeout = 10 * time.Second
	retryConfig := configretry.NewDefaultBackOffConfig()
	retryConfig.MaxElapsedTime = 60 * time.Second
	return &AccountableDeliveryConfig{
		ClientConfig:   httpConfig,
		RetryOnFailure: retryConfig,
		SendingQueue: AccountableDeliveryQueueConfig{
			Enabled:      true,
			NumConsumers: 10,
			Sizer:        "items",
			QueueSize:    10000,
		},
	}
}

type accountableTracesExporter struct {
	consumer.Traces
	delivery *accountableDelivery
}

func (exporterComponent *accountableTracesExporter) Start(ctx context.Context, host component.Host) error {
	return exporterComponent.delivery.Start(ctx, host)
}

func (exporterComponent *accountableTracesExporter) Shutdown(ctx context.Context) error {
	return exporterComponent.delivery.Shutdown(ctx)
}

func (exporterComponent *accountableTracesExporter) DeliveryCounters() DeliveryCounters {
	return exporterComponent.delivery.DeliveryCounters()
}

type accountableMetricsExporter struct {
	consumer.Metrics
	delivery *accountableDelivery
}

func (exporterComponent *accountableMetricsExporter) Start(ctx context.Context, host component.Host) error {
	return exporterComponent.delivery.Start(ctx, host)
}

func (exporterComponent *accountableMetricsExporter) Shutdown(ctx context.Context) error {
	return exporterComponent.delivery.Shutdown(ctx)
}

func (exporterComponent *accountableMetricsExporter) DeliveryCounters() DeliveryCounters {
	return exporterComponent.delivery.DeliveryCounters()
}

type accountableLogsExporter struct {
	consumer.Logs
	delivery *accountableDelivery
}

func (exporterComponent *accountableLogsExporter) Start(ctx context.Context, host component.Host) error {
	return exporterComponent.delivery.Start(ctx, host)
}

func (exporterComponent *accountableLogsExporter) Shutdown(ctx context.Context) error {
	return exporterComponent.delivery.Shutdown(ctx)
}

func (exporterComponent *accountableLogsExporter) DeliveryCounters() DeliveryCounters {
	return exporterComponent.delivery.DeliveryCounters()
}

func createAccountableTracesExporter(_ context.Context, settings exporter.Settings, rawConfig component.Config) (exporter.Traces, error) {
	config, err := checkedAccountableDeliveryConfig(rawConfig)
	if err != nil {
		return nil, err
	}
	delivery := newAccountableDelivery(config, settings, traceDeliverySignal())
	traces, err := consumer.NewTraces(delivery.consumeTraces, consumer.WithCapabilities(consumer.Capabilities{MutatesData: false}))
	if err != nil {
		return nil, err
	}
	return &accountableTracesExporter{Traces: traces, delivery: delivery}, nil
}

func createAccountableMetricsExporter(_ context.Context, settings exporter.Settings, rawConfig component.Config) (exporter.Metrics, error) {
	config, err := checkedAccountableDeliveryConfig(rawConfig)
	if err != nil {
		return nil, err
	}
	delivery := newAccountableDelivery(config, settings, metricDeliverySignal())
	metrics, err := consumer.NewMetrics(delivery.consumeMetrics, consumer.WithCapabilities(consumer.Capabilities{MutatesData: false}))
	if err != nil {
		return nil, err
	}
	return &accountableMetricsExporter{Metrics: metrics, delivery: delivery}, nil
}

func createAccountableLogsExporter(_ context.Context, settings exporter.Settings, rawConfig component.Config) (exporter.Logs, error) {
	config, err := checkedAccountableDeliveryConfig(rawConfig)
	if err != nil {
		return nil, err
	}
	delivery := newAccountableDelivery(config, settings, logDeliverySignal())
	logs, err := consumer.NewLogs(delivery.consumeLogs, consumer.WithCapabilities(consumer.Capabilities{MutatesData: false}))
	if err != nil {
		return nil, err
	}
	return &accountableLogsExporter{Logs: logs, delivery: delivery}, nil
}

func checkedAccountableDeliveryConfig(rawConfig component.Config) (*AccountableDeliveryConfig, error) {
	config, ok := rawConfig.(*AccountableDeliveryConfig)
	if !ok {
		return nil, contractError("accountable_delivery config")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return config, nil
}

type deliverySignal struct {
	name           string
	pipeline       string
	path           string
	decodeResponse func([]byte) (int64, error)
}

func traceDeliverySignal() deliverySignal {
	return deliverySignal{
		name:     "traces",
		pipeline: "traces/app",
		path:     "/v1/traces",
		decodeResponse: func(raw []byte) (int64, error) {
			response := ptraceotlp.NewExportResponse()
			if err := response.UnmarshalProto(raw); err != nil {
				return 0, err
			}
			return response.PartialSuccess().RejectedSpans(), nil
		},
	}
}

func metricDeliverySignal() deliverySignal {
	return deliverySignal{
		name:     "metrics",
		pipeline: "metrics/app",
		path:     "/v1/metrics",
		decodeResponse: func(raw []byte) (int64, error) {
			response := pmetricotlp.NewExportResponse()
			if err := response.UnmarshalProto(raw); err != nil {
				return 0, err
			}
			return response.PartialSuccess().RejectedDataPoints(), nil
		},
	}
}

func logDeliverySignal() deliverySignal {
	return deliverySignal{
		name:     "logs",
		pipeline: "logs/app",
		path:     "/v1/logs",
		decodeResponse: func(raw []byte) (int64, error) {
			response := plogotlp.NewExportResponse()
			if err := response.UnmarshalProto(raw); err != nil {
				return 0, err
			}
			return response.PartialSuccess().RejectedLogRecords(), nil
		},
	}
}

type deliveryEntry struct {
	id         uint64
	body       []byte
	itemCount  int64
	acceptedAt time.Time
}

type deliveryResult struct {
	sent    int64
	dropped int64
}

type accountableDelivery struct {
	config   *AccountableDeliveryConfig
	settings exporter.Settings
	signal   deliverySignal

	mutex       sync.Mutex
	condition   *sync.Cond
	queue       []deliveryEntry
	queuedItems int64
	pending     map[uint64]deliveryEntry
	nextID      uint64
	accepted    int64
	sent        int64
	dropped     int64
	started     bool
	accepting   bool
	shutdown    bool

	client     *http.Client
	runContext context.Context
	cancel     context.CancelFunc
	workers    sync.WaitGroup
	done       chan struct{}
	metrics    deliveryMetrics
	clock      clock.Clock
}

type deliveryMetrics struct {
	accepted     metric.Int64Counter
	sent         metric.Int64Counter
	dropped      metric.Int64Counter
	registration metric.Registration
}

func newAccountableDelivery(config *AccountableDeliveryConfig, settings exporter.Settings, signal deliverySignal) *accountableDelivery {
	delivery := &accountableDelivery{
		config:   config,
		settings: settings,
		signal:   signal,
		pending:  make(map[uint64]deliveryEntry),
		done:     make(chan struct{}),
		clock:    clock.System{},
	}
	delivery.condition = sync.NewCond(&delivery.mutex)
	return delivery
}

func (delivery *accountableDelivery) Start(ctx context.Context, host component.Host) error {
	delivery.mutex.Lock()
	if delivery.started || delivery.shutdown {
		delivery.mutex.Unlock()
		return contractError("accountable_delivery lifecycle")
	}
	delivery.mutex.Unlock()
	var extensions map[component.ID]component.Component
	if host != nil {
		extensions = host.GetExtensions()
	}
	client, err := delivery.config.ToClient(ctx, extensions, delivery.settings.TelemetrySettings)
	if err != nil {
		return fmt.Errorf("start accountable_delivery HTTP client: %w", err)
	}
	delivery.client = client
	if err := delivery.registerMetrics(); err != nil {
		client.CloseIdleConnections()
		return err
	}
	delivery.mutex.Lock()
	defer delivery.mutex.Unlock()
	if delivery.started || delivery.shutdown {
		delivery.unregisterMetrics()
		client.CloseIdleConnections()
		return contractError("accountable_delivery lifecycle")
	}
	delivery.runContext, delivery.cancel = context.WithCancel(context.Background())
	delivery.started = true
	delivery.accepting = true
	for range delivery.config.SendingQueue.NumConsumers {
		delivery.workers.Add(1)
		go delivery.runWorker()
	}
	go func() {
		delivery.workers.Wait()
		close(delivery.done)
	}()
	return nil
}

func (delivery *accountableDelivery) consumeTraces(_ context.Context, traces ptrace.Traces) error {
	raw, err := ptraceotlp.NewExportRequestFromTraces(traces).MarshalProto()
	if err != nil {
		return consumererror.NewPermanent(err)
	}
	return delivery.enqueue(raw, int64(traces.SpanCount()))
}

func (delivery *accountableDelivery) consumeMetrics(_ context.Context, metrics pmetric.Metrics) error {
	raw, err := pmetricotlp.NewExportRequestFromMetrics(metrics).MarshalProto()
	if err != nil {
		return consumererror.NewPermanent(err)
	}
	return delivery.enqueue(raw, int64(metrics.DataPointCount()))
}

func (delivery *accountableDelivery) consumeLogs(_ context.Context, logs plog.Logs) error {
	raw, err := plogotlp.NewExportRequestFromLogs(logs).MarshalProto()
	if err != nil {
		return consumererror.NewPermanent(err)
	}
	return delivery.enqueue(raw, int64(logs.LogRecordCount()))
}

func (delivery *accountableDelivery) enqueue(raw []byte, itemCount int64) error {
	if itemCount == 0 {
		return nil
	}
	delivery.mutex.Lock()
	if !delivery.accepting {
		delivery.mutex.Unlock()
		return errors.New("accountable_delivery is not accepting data")
	}
	if itemCount > delivery.config.SendingQueue.QueueSize ||
		delivery.queuedItems+itemCount > delivery.config.SendingQueue.QueueSize {
		delivery.mutex.Unlock()
		return errors.New("accountable_delivery queue is full")
	}
	delivery.nextID++
	entry := deliveryEntry{
		id:         delivery.nextID,
		body:       raw,
		itemCount:  itemCount,
		acceptedAt: delivery.clock.Now(),
	}
	delivery.queue = append(delivery.queue, entry)
	delivery.queuedItems += itemCount
	delivery.pending[entry.id] = entry
	delivery.accepted += itemCount
	delivery.condition.Signal()
	delivery.mutex.Unlock()
	if delivery.metrics.accepted != nil {
		delivery.metrics.accepted.Add(context.Background(), itemCount, metric.WithAttributes(delivery.counterAttributes()...))
	}
	return nil
}

func (delivery *accountableDelivery) runWorker() {
	defer delivery.workers.Done()
	for {
		entry, ok := delivery.take()
		if !ok {
			return
		}
		result, err := delivery.deliver(entry)
		delivery.complete(entry, result, err)
	}
}

func (delivery *accountableDelivery) take() (deliveryEntry, bool) {
	delivery.mutex.Lock()
	defer delivery.mutex.Unlock()
	for len(delivery.queue) == 0 && delivery.accepting {
		delivery.condition.Wait()
	}
	if len(delivery.queue) == 0 {
		return deliveryEntry{}, false
	}
	entry := delivery.queue[0]
	delivery.queue[0] = deliveryEntry{}
	delivery.queue = delivery.queue[1:]
	delivery.queuedItems -= entry.itemCount
	return entry, true
}

func (delivery *accountableDelivery) deliver(entry deliveryEntry) (deliveryResult, error) {
	retryContext, cancel := context.WithTimeout(delivery.runContext, delivery.config.RetryOnFailure.MaxElapsedTime)
	defer cancel()
	backOff := backoff.NewExponentialBackOff()
	backOff.InitialInterval = delivery.config.RetryOnFailure.InitialInterval
	backOff.RandomizationFactor = delivery.config.RetryOnFailure.RandomizationFactor
	backOff.Multiplier = delivery.config.RetryOnFailure.Multiplier
	backOff.MaxInterval = delivery.config.RetryOnFailure.MaxInterval
	return backoff.Retry(
		retryContext,
		func() (deliveryResult, error) {
			result, retryable, err := delivery.send(retryContext, entry)
			if err != nil && !retryable {
				return result, backoff.Permanent(err)
			}
			return result, err
		},
		backoff.WithBackOff(backOff),
		backoff.WithMaxElapsedTime(delivery.config.RetryOnFailure.MaxElapsedTime),
	)
}

func (delivery *accountableDelivery) send(ctx context.Context, entry deliveryEntry) (deliveryResult, bool, error) {
	endpoint := strings.TrimRight(delivery.config.Endpoint, "/") + delivery.signal.path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(entry.body))
	if err != nil {
		return deliveryResult{}, false, err
	}
	request.Header.Set("Content-Type", "application/x-protobuf")
	response, err := delivery.client.Do(request)
	if err != nil {
		return deliveryResult{}, true, err
	}
	defer func() {
		_ = response.Body.Close()
	}()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil {
		return deliveryResult{}, true, err
	}
	if len(raw) > 1<<20 {
		return deliveryResult{}, true, errors.New("accountable_delivery response exceeds 1 MiB")
	}
	if response.StatusCode != http.StatusOK {
		classified := consumererror.NewOTLPHTTPError(fmt.Errorf("grafana OTLP HTTP status %d", response.StatusCode), response.StatusCode)
		var consumerError *consumererror.Error
		retryable := errors.As(classified, &consumerError) && consumerError.IsRetryable()
		return deliveryResult{}, retryable, classified
	}
	rejected, err := delivery.signal.decodeResponse(raw)
	if err != nil {
		return deliveryResult{}, true, fmt.Errorf("decode Grafana OTLP response: %w", err)
	}
	if rejected < 0 || rejected > entry.itemCount {
		return deliveryResult{}, true, errors.New("grafana OTLP partial success has an invalid rejected item count")
	}
	return deliveryResult{sent: entry.itemCount - rejected, dropped: rejected}, false, nil
}

func (delivery *accountableDelivery) complete(entry deliveryEntry, result deliveryResult, deliveryError error) {
	delivery.mutex.Lock()
	if deliveryError != nil {
		if delivery.runContext.Err() != nil {
			delivery.mutex.Unlock()
			return
		}
		result = deliveryResult{dropped: entry.itemCount}
	}
	delivery.sent += result.sent
	delivery.dropped += result.dropped
	delete(delivery.pending, entry.id)
	delivery.mutex.Unlock()
	attributes := metric.WithAttributes(delivery.counterAttributes()...)
	if result.sent > 0 && delivery.metrics.sent != nil {
		delivery.metrics.sent.Add(context.Background(), result.sent, attributes)
	}
	if result.dropped > 0 && delivery.metrics.dropped != nil {
		delivery.metrics.dropped.Add(context.Background(), result.dropped, attributes)
	}
}

func (delivery *accountableDelivery) Shutdown(ctx context.Context) error {
	delivery.mutex.Lock()
	if delivery.shutdown {
		done := delivery.done
		delivery.mutex.Unlock()
		select {
		case <-done:
			return delivery.validateCompleteDelivery()
		case <-ctx.Done():
			return fmt.Errorf("accountable_delivery shutdown: %w", context.Cause(ctx))
		}
	}
	delivery.shutdown = true
	delivery.accepting = false
	started := delivery.started
	delivery.condition.Broadcast()
	done := delivery.done
	delivery.mutex.Unlock()
	if !started {
		close(done)
		return nil
	}
	select {
	case <-done:
		delivery.cancel()
		delivery.client.CloseIdleConnections()
		delivery.unregisterMetrics()
		return delivery.validateCompleteDelivery()
	case <-ctx.Done():
		delivery.cancel()
		delivery.condition.Broadcast()
		<-done
		delivery.client.CloseIdleConnections()
		delivery.unregisterMetrics()
		return fmt.Errorf("accountable_delivery shutdown left unconfirmed items: %w", context.Cause(ctx))
	}
}

func (delivery *accountableDelivery) DeliveryCounters() DeliveryCounters {
	delivery.mutex.Lock()
	defer delivery.mutex.Unlock()
	counters := DeliveryCounters{
		AcceptedItemCount:           delivery.accepted,
		SentItemCount:               delivery.sent,
		PermanentlyDroppedItemCount: delivery.dropped,
	}
	now := delivery.clock.Now()
	for _, entry := range delivery.pending {
		counters.PendingItemCount += entry.itemCount
		age := int64(now.Sub(entry.acceptedAt) / time.Second)
		if age > counters.OldestPendingItemAgeSeconds {
			counters.OldestPendingItemAgeSeconds = age
		}
	}
	return counters
}

func (delivery *accountableDelivery) validateConservation() error {
	if err := ValidateDeliveryCounters(delivery.DeliveryCounters()); err != nil {
		if delivery.settings.Logger != nil {
			delivery.settings.Logger.Error("telemetry.accounting_uncertain")
		}
		return fmt.Errorf("accountable_delivery accounting is uncertain: %w", err)
	}
	return nil
}

func (delivery *accountableDelivery) validateCompleteDelivery() error {
	if err := delivery.validateConservation(); err != nil {
		return err
	}
	if delivery.DeliveryCounters().PendingItemCount != 0 {
		err := errors.New("accepted items do not have confirmed terminal outcomes")
		if delivery.settings.Logger != nil {
			delivery.settings.Logger.Error("telemetry.accounting_uncertain")
		}
		return fmt.Errorf("accountable_delivery accounting is uncertain: %w", err)
	}
	return nil
}

func (delivery *accountableDelivery) registerMetrics() error {
	if delivery.settings.MeterProvider == nil {
		return nil
	}
	meter := delivery.settings.MeterProvider.Meter("github.com/acctbl/accountable/internal/platform/telemetry/accountable_delivery")
	accepted, err := meter.Int64Counter("AcceptedItemCount")
	if err != nil {
		return err
	}
	sent, err := meter.Int64Counter("SentItemCount")
	if err != nil {
		return err
	}
	dropped, err := meter.Int64Counter("PermanentlyDroppedItemCount")
	if err != nil {
		return err
	}
	pending, err := meter.Int64ObservableGauge("PendingItemCount")
	if err != nil {
		return err
	}
	oldest, err := meter.Int64ObservableGauge("OldestPendingItemAgeSeconds")
	if err != nil {
		return err
	}
	registration, err := meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		counters := delivery.DeliveryCounters()
		attributes := metric.WithAttributes(delivery.gaugeAttributes()...)
		observer.ObserveInt64(pending, counters.PendingItemCount, attributes)
		observer.ObserveInt64(oldest, counters.OldestPendingItemAgeSeconds, attributes)
		return nil
	}, pending, oldest)
	if err != nil {
		return err
	}
	delivery.metrics = deliveryMetrics{
		accepted:     accepted,
		sent:         sent,
		dropped:      dropped,
		registration: registration,
	}
	return nil
}

func (delivery *accountableDelivery) unregisterMetrics() {
	if delivery.metrics.registration != nil {
		_ = delivery.metrics.registration.Unregister()
	}
}

func (delivery *accountableDelivery) counterAttributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("Environment", delivery.config.Environment),
		attribute.String("CellId", delivery.config.CellID),
		attribute.String("Pipeline", delivery.signal.pipeline),
		attribute.String("Signal", delivery.signal.name),
	}
}

func (delivery *accountableDelivery) gaugeAttributes() []attribute.KeyValue {
	return append(delivery.counterAttributes(), attribute.String("CollectorTaskId", delivery.config.CollectorTaskID))
}
