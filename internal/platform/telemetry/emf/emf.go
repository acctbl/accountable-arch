package emf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"

	"github.com/acctbl/accountable/internal/platform/telemetry"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

const (
	modeSLI      = "sli"
	modeInternal = "internal"
	maxBatchSize = 1 << 20
	maxEventSize = 256 << 10
)

var (
	accountableEMFType = component.MustNewType("accountable_emf")
	regionPattern      = regexp.MustCompile(`^[a-z]{2}-[a-z]+-[0-9]+$`)
)

type Config struct {
	Region        string `mapstructure:"region"`
	Mode          string `mapstructure:"mode"`
	Namespace     string `mapstructure:"namespace"`
	LogGroupName  string `mapstructure:"log_group_name"`
	LogStreamName string `mapstructure:"log_stream_name"`
	Environment   string `mapstructure:"environment"`
	CellID        string `mapstructure:"cell_id"`
	client        logsClient
}

func (config *Config) Validate() error {
	if !regionPattern.MatchString(config.Region) ||
		(config.Mode != modeSLI && config.Mode != modeInternal) ||
		config.Namespace == "" || config.LogGroupName == "" || config.LogStreamName == "" {
		return contractError("configuration")
	}
	if err := telemetry.ValidateCollectorIdentity(config.Environment, config.CellID, config.LogStreamName); err != nil {
		return err
	}
	if config.Mode == modeSLI && config.Namespace != telemetry.SLONamespace {
		return contractError("SLI namespace")
	}
	if config.Mode == modeInternal && config.Namespace != telemetry.CollectorNamespace {
		return contractError("internal namespace")
	}
	return nil
}

func contractError(field string) error {
	return fmt.Errorf("%w: accountable_emf %s", telemetry.ErrInvalidContract, field)
}

func NewFactory() exporter.Factory {
	return exporter.NewFactory(
		accountableEMFType,
		func() component.Config { return &Config{} },
		exporter.WithMetrics(createMetricsExporter, component.StabilityLevelAlpha),
	)
}

type logsClient interface {
	CreateLogStream(context.Context, *cloudwatchlogs.CreateLogStreamInput, ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.CreateLogStreamOutput, error)
	PutLogEvents(context.Context, *cloudwatchlogs.PutLogEventsInput, ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.PutLogEventsOutput, error)
}

type metricsExporter struct {
	consumer.Metrics
	config *Config
	client logsClient
	logger *telemetry.SafeLogger
}

func createMetricsExporter(_ context.Context, _ exporter.Settings, rawConfig component.Config) (exporter.Metrics, error) {
	config, ok := rawConfig.(*Config)
	if !ok {
		return nil, contractError("configuration type")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	created := &metricsExporter{
		config: config,
		client: config.client,
		logger: telemetry.NewSafeLogger(telemetry.SafeLoggerOptions{Stdout: os.Stdout}),
	}
	metrics, err := consumer.NewMetrics(created.consume, consumer.WithCapabilities(consumer.Capabilities{MutatesData: false}))
	if err != nil {
		return nil, err
	}
	created.Metrics = metrics
	return created, nil
}

func (created *metricsExporter) Start(ctx context.Context, _ component.Host) error {
	if created.client == nil {
		options := []func(*awsconfig.LoadOptions) error{
			awsconfig.WithRegion(created.config.Region),
		}
		if created.config.Mode == modeSLI {
			options = append(options, awsconfig.WithRetryer(func() aws.Retryer { return aws.NopRetryer{} }))
		} else {
			options = append(options, awsconfig.WithRetryMaxAttempts(3))
		}
		config, err := awsconfig.LoadDefaultConfig(ctx, options...)
		if err != nil {
			return fmt.Errorf("load accountable_emf AWS configuration: %w", err)
		}
		created.client = cloudwatchlogs.NewFromConfig(config)
	}
	_, err := created.client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{
		LogGroupName:  aws.String(created.config.LogGroupName),
		LogStreamName: aws.String(created.config.LogStreamName),
	})
	var exists *types.ResourceAlreadyExistsException
	if err != nil && !errors.As(err, &exists) {
		created.reportFailure()
		return fmt.Errorf("create accountable_emf log stream: %w", err)
	}
	return nil
}

func (created *metricsExporter) Shutdown(context.Context) error { return nil }

func (created *metricsExporter) consume(ctx context.Context, metrics pmetric.Metrics) error {
	if created.client == nil {
		return consumererror.NewPermanent(errors.New("accountable_emf is not started"))
	}
	events, err := buildEvents(created.config, metrics)
	if err != nil {
		created.reportFailure()
		return consumererror.NewPermanent(err)
	}
	if len(events) == 0 {
		return nil
	}
	output, err := created.client.PutLogEvents(ctx, &cloudwatchlogs.PutLogEventsInput{
		LogEvents:     events,
		LogGroupName:  aws.String(created.config.LogGroupName),
		LogStreamName: aws.String(created.config.LogStreamName),
	})
	if err != nil {
		created.reportFailure()
		return err
	}
	if output == nil || output.RejectedLogEventsInfo != nil || output.RejectedEntityInfo != nil {
		created.reportFailure()
		return errors.New("accountable_emf delivery was not fully confirmed")
	}
	return nil
}

func (created *metricsExporter) reportFailure() {
	eventName := "telemetry.accounting_uncertain"
	if created.config.Mode == modeSLI {
		eventName = "telemetry.sli_delivery_uncertain"
	}
	_ = created.logger.EmitPreVerification(context.Background(), eventName, map[string]string{
		telemetry.StdoutFieldEnvironment: created.config.Environment,
		telemetry.StdoutFieldCellID:      created.config.CellID,
	})
}

type emfMetadata struct {
	Timestamp         int64                `json:"Timestamp"`
	CloudWatchMetrics []emfMetricDirective `json:"CloudWatchMetrics"`
}

type emfMetricDirective struct {
	Namespace  string                `json:"Namespace"`
	Dimensions [][]string            `json:"Dimensions"`
	Metrics    []emfMetricDefinition `json:"Metrics"`
}

type emfMetricDefinition struct {
	Name string `json:"Name"`
	Unit string `json:"Unit,omitempty"`
}

func buildEvents(config *Config, metrics pmetric.Metrics) ([]types.InputLogEvent, error) {
	events := make([]types.InputLogEvent, 0, metrics.DataPointCount())
	totalSize := 0
	for resourceIndex := 0; resourceIndex < metrics.ResourceMetrics().Len(); resourceIndex++ {
		scopes := metrics.ResourceMetrics().At(resourceIndex).ScopeMetrics()
		for scopeIndex := 0; scopeIndex < scopes.Len(); scopeIndex++ {
			batch := scopes.At(scopeIndex).Metrics()
			for metricIndex := 0; metricIndex < batch.Len(); metricIndex++ {
				metricEvents, err := buildMetricEvents(config, batch.At(metricIndex))
				if err != nil {
					return nil, err
				}
				for _, event := range metricEvents {
					size := len(aws.ToString(event.Message)) + 26
					if size > maxEventSize || totalSize+size > maxBatchSize || len(events) == 10000 {
						return nil, contractError("CloudWatch Logs batch size")
					}
					totalSize += size
					events = append(events, event)
				}
			}
		}
	}
	sort.Slice(events, func(left, right int) bool {
		return aws.ToInt64(events[left].Timestamp) < aws.ToInt64(events[right].Timestamp)
	})
	return events, nil
}

func buildMetricEvents(config *Config, metric pmetric.Metric) ([]types.InputLogEvent, error) {
	if metric.Type() != pmetric.MetricTypeSum && metric.Type() != pmetric.MetricTypeGauge {
		return nil, contractError("metric type")
	}
	if config.Mode == modeSLI && (metric.Type() != pmetric.MetricTypeSum ||
		metric.Sum().AggregationTemporality() != pmetric.AggregationTemporalityDelta || !metric.Sum().IsMonotonic()) {
		return nil, contractError("SLI temporality")
	}
	if metric.Type() == pmetric.MetricTypeSum {
		return buildNumberEvents(config, metric.Name(), metric.Sum().DataPoints())
	}
	return buildNumberEvents(config, metric.Name(), metric.Gauge().DataPoints())
}

func buildNumberEvents(config *Config, name string, points pmetric.NumberDataPointSlice) ([]types.InputLogEvent, error) {
	dimensions, unit, err := metricContract(config.Mode, name)
	if err != nil {
		return nil, err
	}
	events := make([]types.InputLogEvent, 0, points.Len())
	for index := 0; index < points.Len(); index++ {
		point := points.At(index)
		if point.ValueType() != pmetric.NumberDataPointValueTypeInt || point.IntValue() < 0 {
			return nil, contractError("metric value")
		}
		attributes, err := stringAttributes(point.Attributes(), dimensions)
		if err != nil {
			return nil, err
		}
		timestamp := point.Timestamp().AsTime()
		if timestamp.IsZero() {
			return nil, contractError("metric timestamp")
		}
		document := make(map[string]any, len(attributes)+2)
		for key, value := range attributes {
			document[key] = value
		}
		document[name] = point.IntValue()
		document["_aws"] = emfMetadata{
			Timestamp: timestamp.UnixMilli(),
			CloudWatchMetrics: []emfMetricDirective{{
				Namespace:  config.Namespace,
				Dimensions: [][]string{dimensions},
				Metrics:    []emfMetricDefinition{{Name: name, Unit: unit}},
			}},
		}
		raw, err := json.Marshal(document)
		if err != nil {
			return nil, err
		}
		events = append(events, types.InputLogEvent{Message: aws.String(string(raw)), Timestamp: aws.Int64(timestamp.UnixMilli())})
	}
	return events, nil
}

func stringAttributes(attributes pcommon.Map, expected []string) (map[string]string, error) {
	if attributes.Len() != len(expected) {
		return nil, contractError("metric dimensions")
	}
	result := make(map[string]string, len(expected))
	for _, name := range expected {
		value, present := attributes.Get(name)
		if !present || value.Type() != pcommon.ValueTypeStr || value.Str() == "" {
			return nil, contractError("metric dimensions")
		}
		result[name] = value.Str()
	}
	return result, nil
}

func metricContract(mode, name string) ([]string, string, error) {
	if mode == modeSLI {
		switch name {
		case "EligibleRequestCount", "AvailabilityBadRequestCount", "LatencySlowRequestCount":
			return []string{"Environment", "ServiceName"}, "Count", nil
		case "SLIExportHeartbeatCount":
			return []string{"Environment", "ServiceName", "CellId"}, "Count", nil
		default:
			return nil, "", contractError("SLI metric name")
		}
	}
	switch name {
	case "AcceptedItemCount", "RefusedItemCount", "SentItemCount", "PermanentlyDroppedItemCount":
		return []string{"Environment", "CellId", "Pipeline", "Signal"}, "Count", nil
	case "PendingItemCount":
		return []string{"Environment", "CellId", "Pipeline", "Signal", "CollectorTaskId"}, "Count", nil
	case "OldestPendingItemAgeSeconds":
		return []string{"Environment", "CellId", "Pipeline", "Signal", "CollectorTaskId"}, "Seconds", nil
	case "CollectorUptimeSeconds":
		return []string{"Environment", "CellId", "CollectorTaskId"}, "Seconds", nil
	case "CollectorMemoryRssBytes":
		return []string{"Environment", "CellId", "CollectorTaskId"}, "Bytes", nil
	default:
		return nil, "", contractError("internal metric name")
	}
}
