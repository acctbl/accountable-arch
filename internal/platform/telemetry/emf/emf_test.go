package emf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/acctbl/accountable/internal/platform/telemetry"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

type recordingClient struct {
	createInputs []*cloudwatchlogs.CreateLogStreamInput
	inputs       []*cloudwatchlogs.PutLogEventsInput
	output       *cloudwatchlogs.PutLogEventsOutput
	err          error
}

func (client *recordingClient) CreateLogStream(
	_ context.Context,
	input *cloudwatchlogs.CreateLogStreamInput,
	_ ...func(*cloudwatchlogs.Options),
) (*cloudwatchlogs.CreateLogStreamOutput, error) {
	client.createInputs = append(client.createInputs, input)
	return &cloudwatchlogs.CreateLogStreamOutput{}, nil
}

func (client *recordingClient) PutLogEvents(
	_ context.Context,
	input *cloudwatchlogs.PutLogEventsInput,
	_ ...func(*cloudwatchlogs.Options),
) (*cloudwatchlogs.PutLogEventsOutput, error) {
	client.inputs = append(client.inputs, input)
	return client.output, client.err
}

func TestSLIExporterMakesOneStrictEMFRequest(t *testing.T) {
	client := &recordingClient{output: &cloudwatchlogs.PutLogEventsOutput{}}
	config := testConfig(modeSLI, client)
	created := testExporter(config, client, new(bytes.Buffer))

	metrics := pmetric.NewMetrics()
	metric := metrics.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	metric.SetName("EligibleRequestCount")
	sum := metric.SetEmptySum()
	sum.SetAggregationTemporality(pmetric.AggregationTemporalityDelta)
	sum.SetIsMonotonic(true)
	point := sum.DataPoints().AppendEmpty()
	point.SetIntValue(7)
	point.SetTimestamp(pcommon.NewTimestampFromTime(time.Unix(100, 0)))
	point.Attributes().PutStr("Environment", "staging")
	point.Attributes().PutStr("ServiceName", "accountable-api")

	if err := created.consume(context.Background(), metrics); err != nil {
		t.Fatal(err)
	}
	if len(client.inputs) != 1 || len(client.inputs[0].LogEvents) != 1 {
		t.Fatalf("PutLogEvents calls=%d events=%d", len(client.inputs), len(client.inputs[0].LogEvents))
	}
	var document map[string]any
	if err := json.Unmarshal([]byte(*client.inputs[0].LogEvents[0].Message), &document); err != nil {
		t.Fatal(err)
	}
	if document["EligibleRequestCount"] != float64(7) || document["Environment"] != "staging" ||
		document["ServiceName"] != "accountable-api" {
		t.Fatalf("unexpected EMF document: %v", document)
	}
}

func TestExporterCreatesItsTaskScopedStream(t *testing.T) {
	client := &recordingClient{output: &cloudwatchlogs.PutLogEventsOutput{}}
	created := testExporter(testConfig(modeSLI, client), client, new(bytes.Buffer))
	if err := created.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(client.createInputs) != 1 ||
		*client.createInputs[0].LogGroupName != "/accountable/stg-a/emf-sli" ||
		*client.createInputs[0].LogStreamName != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("create inputs = %v", client.createInputs)
	}
}

func TestSLIExporterRejectsAnythingThatCouldDuplicateOrWidenTheSLI(t *testing.T) {
	for name, mutate := range map[string]func(pmetric.Metric){
		"cumulative": func(metric pmetric.Metric) {
			metric.Sum().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
		},
		"non-monotonic": func(metric pmetric.Metric) { metric.Sum().SetIsMonotonic(false) },
		"extra dimension": func(metric pmetric.Metric) {
			metric.Sum().DataPoints().At(0).Attributes().PutStr("ReleaseId", "unbounded")
		},
		"negative": func(metric pmetric.Metric) { metric.Sum().DataPoints().At(0).SetIntValue(-1) },
	} {
		t.Run(name, func(t *testing.T) {
			client := &recordingClient{output: &cloudwatchlogs.PutLogEventsOutput{}}
			created := testExporter(testConfig(modeSLI, client), client, new(bytes.Buffer))
			metrics := validSLIMetrics()
			mutate(metrics.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics().At(0))
			if err := created.consume(context.Background(), metrics); !errors.Is(err, telemetry.ErrInvalidContract) {
				t.Fatalf("got %v", err)
			}
			if len(client.inputs) != 0 {
				t.Fatal("invalid SLI data reached CloudWatch Logs")
			}
		})
	}
}

func TestUnconfirmedSLIDeliveryEmitsIndependentUncertainty(t *testing.T) {
	client := &recordingClient{output: &cloudwatchlogs.PutLogEventsOutput{
		RejectedLogEventsInfo: &types.RejectedLogEventsInfo{TooNewLogEventStartIndex: new(int32)},
	}}
	var stdout bytes.Buffer
	created := testExporter(testConfig(modeSLI, client), client, &stdout)
	if err := created.consume(context.Background(), validSLIMetrics()); err == nil {
		t.Fatal("a partial CloudWatch Logs result must fail")
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"event_name":"telemetry.sli_delivery_uncertain"`)) {
		t.Fatalf("uncertainty record missing: %s", stdout.String())
	}
}

func testConfig(mode string, client logsClient) *Config {
	namespace := telemetry.SLONamespace
	if mode == modeInternal {
		namespace = telemetry.CollectorNamespace
	}
	return &Config{
		Region:        "eu-west-2",
		Mode:          mode,
		Namespace:     namespace,
		LogGroupName:  "/accountable/stg-a/emf-" + mode,
		LogStreamName: "0123456789abcdef0123456789abcdef",
		Environment:   "staging",
		CellID:        "stg-a",
		client:        client,
	}
}

func testExporter(config *Config, client logsClient, stdout *bytes.Buffer) *metricsExporter {
	return &metricsExporter{
		config: config,
		client: client,
		logger: telemetry.NewSafeLogger(telemetry.SafeLoggerOptions{Stdout: stdout}),
	}
}

func validSLIMetrics() pmetric.Metrics {
	metrics := pmetric.NewMetrics()
	metric := metrics.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	metric.SetName("EligibleRequestCount")
	sum := metric.SetEmptySum()
	sum.SetAggregationTemporality(pmetric.AggregationTemporalityDelta)
	sum.SetIsMonotonic(true)
	point := sum.DataPoints().AppendEmpty()
	point.SetIntValue(1)
	point.SetTimestamp(pcommon.NewTimestampFromTime(time.Unix(100, 0)))
	point.Attributes().PutStr("Environment", "staging")
	point.Attributes().PutStr("ServiceName", "accountable-api")
	return metrics
}
