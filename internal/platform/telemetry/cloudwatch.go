package telemetry

const (
	SLONamespace                 = "Accountable/SLO"
	CollectorNamespace           = "Accountable/Collector"
	ClientTelemetryNamespace     = "Accountable/ClientTelemetry"
	ObservabilityNamespace       = "Accountable/Observability"
	SLIDeliveryUncertainName     = "SLIDeliveryUncertain"
	CollectorDeliveryUncertain   = "CollectorDeliveryUncertain"
	CollectorAccountingUncertain = "CollectorAccountingUncertain"
	ClientEventDroppedName       = "ClientEventDropped"
	ClientEventUncertainName     = "ClientEventDeliveryUncertain"
)

type cloudWatchMapping struct {
	Source string
	Metric string
}

// sliMetricMappings is the only permitted source-to-CloudWatch mapping for the
// SLI pipeline. Order is stable so generated configuration stays byte-identical.
var sliMetricMappings = []cloudWatchMapping{
	{Source: "accountable.sli.request.eligible", Metric: "EligibleRequestCount"},
	{Source: "accountable.sli.availability.bad", Metric: "AvailabilityBadRequestCount"},
	{Source: "accountable.sli.latency.slow", Metric: "LatencySlowRequestCount"},
	{Source: "accountable.sli.export.heartbeat", Metric: "SLIExportHeartbeatCount"},
}

const sliHeartbeatMetric = "SLIExportHeartbeatCount"

// internalMetricMappings maps scraped Collector self-metrics to the delivery and
// health metrics that CloudWatch owns. The scrape names carry the internal
// `otelcol_` namespace and the Prometheus counter suffix.
var internalMetricMappings = []cloudWatchMapping{
	{Source: "otelcol_AcceptedItemCount_total", Metric: "AcceptedItemCount"},
	{Source: "otelcol_SentItemCount_total", Metric: "SentItemCount"},
	{Source: "otelcol_PermanentlyDroppedItemCount_total", Metric: "PermanentlyDroppedItemCount"},
	{Source: "otelcol_PendingItemCount", Metric: "PendingItemCount"},
	{Source: "otelcol_OldestPendingItemAgeSeconds", Metric: "OldestPendingItemAgeSeconds"},
	{Source: "otelcol_process_uptime_seconds_total", Metric: "CollectorUptimeSeconds"},
	{Source: "otelcol_process_memory_rss_bytes", Metric: "CollectorMemoryRssBytes"},
}

const refusedSourcePrefix = "otelcol_receiver_refused_"

// refusedItemMappings translates documented receiver-refused self-metrics to the
// bounded pipeline and signal that owns each refusal.
var refusedItemMappings = []struct {
	Source   string
	Receiver string
	Pipeline string
	Signal   string
}{
	{Source: refusedSourcePrefix + "spans_total", Receiver: "otlp/app", Pipeline: "traces/app", Signal: "traces"},
	{Source: refusedSourcePrefix + "metric_points_total", Receiver: "otlp/app", Pipeline: "metrics/app", Signal: "metrics"},
	{Source: refusedSourcePrefix + "log_records_total", Receiver: "otlp/app", Pipeline: "logs/app", Signal: "logs"},
	{Source: refusedSourcePrefix + "metric_points_total", Receiver: "otlp/slo", Pipeline: "metrics/slo", Signal: "metrics"},
}

var collectorHealthMetrics = []string{"otelcol_process_uptime_seconds_total", "otelcol_process_memory_rss_bytes"}

func internalSourceNames() []string {
	names := make([]string, 0, len(internalMetricMappings)+len(refusedItemMappings))
	for _, mapping := range internalMetricMappings {
		names = append(names, mapping.Source)
	}
	for _, mapping := range refusedItemMappings {
		if !contains(names, mapping.Source) {
			names = append(names, mapping.Source)
		}
	}
	return names
}
