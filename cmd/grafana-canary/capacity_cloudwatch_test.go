package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cloudwatchtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

type fakeCapacityCloudWatch struct {
	getInput *cloudwatch.GetMetricDataInput
	get      *cloudwatch.GetMetricDataOutput
	getErr   error
	putInput *cloudwatch.PutMetricDataInput
	put      *cloudwatch.PutMetricDataOutput
	putErr   error
	putCalls int
}

func (fake *fakeCapacityCloudWatch) GetMetricData(
	_ context.Context,
	input *cloudwatch.GetMetricDataInput,
	_ ...func(*cloudwatch.Options),
) (*cloudwatch.GetMetricDataOutput, error) {
	fake.getInput = input
	return fake.get, fake.getErr
}

func (fake *fakeCapacityCloudWatch) PutMetricData(
	_ context.Context,
	input *cloudwatch.PutMetricDataInput,
	_ ...func(*cloudwatch.Options),
) (*cloudwatch.PutMetricDataOutput, error) {
	fake.putCalls++
	fake.putInput = input
	return fake.put, fake.putErr
}

func TestCloudWatchCapacityStoreReadsTheOldestCompleteRollingSample(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 12, 10, 7, 0, 0, time.UTC)
	logSample := now.Add(-7 * 24 * time.Hour)
	traceSample := now.Add(-6 * 24 * time.Hour)
	fake := &fakeCapacityCloudWatch{get: &cloudwatch.GetMetricDataOutput{
		MetricDataResults: []cloudwatchtypes.MetricDataResult{
			{Id: aws.String("q1"), StatusCode: cloudwatchtypes.StatusCodeComplete, Timestamps: []time.Time{traceSample}, Values: []float64{5.25}},
			{Id: aws.String("q0"), StatusCode: cloudwatchtypes.StatusCodeComplete, Timestamps: []time.Time{logSample, logSample.Add(15 * time.Minute)}, Values: []float64{12.5, 12.75}},
		},
	}}
	store := newCloudWatchCapacityStore(fake)

	usage, err := store.PreviousUsage(context.Background(), now, []string{"trace_ingestion", "log_ingestion"})
	if err != nil {
		t.Fatal(err)
	}
	if usage["log_ingestion"].Value.String() != "12.5" || !usage["log_ingestion"].SampledAt.Equal(logSample) {
		t.Fatalf("log usage = %#v", usage["log_ingestion"])
	}
	if usage["trace_ingestion"].Value.String() != "5.25" || !usage["trace_ingestion"].SampledAt.Equal(traceSample) {
		t.Fatalf("trace usage = %#v", usage["trace_ingestion"])
	}
	if fake.getInput == nil || fake.getInput.ScanBy != cloudwatchtypes.ScanByTimestampAscending ||
		len(fake.getInput.MetricDataQueries) != 2 || fake.getInput.NextToken != nil {
		t.Fatalf("query = %#v", fake.getInput)
	}
	if !fake.getInput.StartTime.Equal(now.Add(-7*24*time.Hour)) || !fake.getInput.EndTime.Equal(now) {
		t.Fatalf("window = %v to %v", fake.getInput.StartTime, fake.getInput.EndTime)
	}
}

func TestCloudWatchCapacityStorePublishesOneAtomicRequest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	fake := &fakeCapacityCloudWatch{put: &cloudwatch.PutMetricDataOutput{}}
	store := newCloudWatchCapacityStore(fake)
	metrics := []capacityMetric{
		newCapacityMetric("CapacityReadSuccess", mustExactNumber("1"), now),
		{Name: "CurrentUsagePercent", Value: mustExactNumber("12.5"), SampledAt: now, Limit: "log_ingestion"},
		{Name: "AllocatedCapacityPercent", Value: mustExactNumber("10"), SampledAt: now, Environment: "development"},
	}

	if err := store.Publish(context.Background(), metrics); err != nil {
		t.Fatal(err)
	}
	if fake.putCalls != 1 || fake.putInput == nil || aws.ToString(fake.putInput.Namespace) != capacityNamespace || len(fake.putInput.MetricData) != 3 {
		t.Fatalf("put calls = %d, input = %#v", fake.putCalls, fake.putInput)
	}
	if aws.ToFloat64(fake.putInput.MetricData[1].Value) != 12.5 || fake.putInput.MetricData[1].Unit != cloudwatchtypes.StandardUnitPercent {
		t.Fatalf("percent datum = %#v", fake.putInput.MetricData[1])
	}
	if dimensionsOf(fake.putInput.MetricData[1]) != "Limit=log_ingestion,StackName=acctbl" {
		t.Fatalf("limit dimensions = %s", dimensionsOf(fake.putInput.MetricData[1]))
	}
	if dimensionsOf(fake.putInput.MetricData[2]) != "Environment=development,StackName=acctbl" {
		t.Fatalf("environment dimensions = %s", dimensionsOf(fake.putInput.MetricData[2]))
	}
}

func TestCloudWatchCapacityStoreRejectsUnconfirmedOrMalformedResults(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	for name, output := range map[string]*cloudwatch.GetMetricDataOutput{
		"nil":          nil,
		"pagination":   {NextToken: aws.String("more")},
		"global error": {Messages: []cloudwatchtypes.MessageData{{Code: aws.String("error")}}},
		"partial": {MetricDataResults: []cloudwatchtypes.MetricDataResult{
			{Id: aws.String("q0"), StatusCode: cloudwatchtypes.StatusCodePartialData},
		}},
		"mismatched values": {MetricDataResults: []cloudwatchtypes.MetricDataResult{
			{Id: aws.String("q0"), StatusCode: cloudwatchtypes.StatusCodeComplete, Timestamps: []time.Time{now}},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			store := newCloudWatchCapacityStore(&fakeCapacityCloudWatch{get: output})
			if _, err := store.PreviousUsage(context.Background(), now, []string{"log_ingestion"}); err == nil {
				t.Fatal("malformed result was accepted")
			}
		})
	}

	fake := &fakeCapacityCloudWatch{put: nil}
	store := newCloudWatchCapacityStore(fake)
	if err := store.Publish(context.Background(), []capacityMetric{newCapacityMetric("CapacityReadSuccess", mustExactNumber("1"), now)}); err == nil {
		t.Fatal("an unconfirmed publish was accepted")
	}
	if fake.putCalls != 1 {
		t.Fatalf("put calls = %d", fake.putCalls)
	}
}

func TestCloudWatchCapacityStoreRejectsInvalidMetricBatchesBeforeSending(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	invalid := map[string][]capacityMetric{
		"empty": nil,
		"mixed timestamps": {
			newCapacityMetric("CapacityReadSuccess", mustExactNumber("1"), now),
			newCapacityMetric("TierPolicyCompliant", mustExactNumber("1"), now.Add(time.Second)),
		},
		"unknown metric": {newCapacityMetric("Unknown", mustExactNumber("1"), now)},
		"missing limit":  {newCapacityMetric("CurrentUsage", mustExactNumber("1"), now)},
	}
	for name, metrics := range invalid {
		t.Run(name, func(t *testing.T) {
			fake := &fakeCapacityCloudWatch{put: &cloudwatch.PutMetricDataOutput{}}
			store := newCloudWatchCapacityStore(fake)
			if err := store.Publish(context.Background(), metrics); err == nil {
				t.Fatal("invalid batch was accepted")
			}
			if fake.putCalls != 0 {
				t.Fatalf("put calls = %d", fake.putCalls)
			}
		})
	}
}

func TestCloudWatchCapacityStoreReturnsProviderErrors(t *testing.T) {
	t.Parallel()

	providerErr := errors.New("provider unavailable")
	store := newCloudWatchCapacityStore(&fakeCapacityCloudWatch{getErr: providerErr, putErr: providerErr})
	if _, err := store.PreviousUsage(context.Background(), time.Now(), []string{"log_ingestion"}); !errors.Is(err, providerErr) {
		t.Fatalf("get error = %v", err)
	}
	if err := store.Publish(context.Background(), []capacityMetric{newCapacityMetric("CapacityReadSuccess", mustExactNumber("1"), time.Now())}); !errors.Is(err, providerErr) {
		t.Fatalf("put error = %v", err)
	}
}

func dimensionsOf(datum cloudwatchtypes.MetricDatum) string {
	value := ""
	for index, dimension := range datum.Dimensions {
		if index > 0 {
			value += ","
		}
		value += aws.ToString(dimension.Name) + "=" + aws.ToString(dimension.Value)
	}
	return value
}
