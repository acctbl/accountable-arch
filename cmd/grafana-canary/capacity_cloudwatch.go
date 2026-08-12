package main

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cloudwatchtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"

	"github.com/acctbl/accountable/internal/platform/telemetry"
)

const capacityHistoryWindow = 7 * 24 * time.Hour

type capacityCloudWatchAPI interface {
	GetMetricData(context.Context, *cloudwatch.GetMetricDataInput, ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricDataOutput, error)
	PutMetricData(context.Context, *cloudwatch.PutMetricDataInput, ...func(*cloudwatch.Options)) (*cloudwatch.PutMetricDataOutput, error)
}

type cloudWatchCapacityStore struct {
	client capacityCloudWatchAPI
}

type capacityMetricContract struct {
	limit       bool
	environment bool
	unit        cloudwatchtypes.StandardUnit
}

var capacityMetricContracts = map[string]capacityMetricContract{
	"AllocatedCapacityPercent": {environment: true, unit: cloudwatchtypes.StandardUnitPercent},
	"CapacityReadSuccess":      {unit: cloudwatchtypes.StandardUnitNone},
	"CapacitySampleFresh":      {unit: cloudwatchtypes.StandardUnitNone},
	"CurrentUsage":             {limit: true, unit: cloudwatchtypes.StandardUnitNone},
	"CurrentUsagePercent":      {limit: true, unit: cloudwatchtypes.StandardUnitPercent},
	"ForecastUsagePercent":     {limit: true, unit: cloudwatchtypes.StandardUnitPercent},
	"HardLimit":                {limit: true, unit: cloudwatchtypes.StandardUnitNone},
	"IRMIncludedAtNoCost":      {unit: cloudwatchtypes.StandardUnitNone},
	"SharedAllocationPercent":  {unit: cloudwatchtypes.StandardUnitPercent},
	"TierPolicyCompliant":      {unit: cloudwatchtypes.StandardUnitNone},
}

func newCloudWatchCapacityStore(client capacityCloudWatchAPI) *cloudWatchCapacityStore {
	return &cloudWatchCapacityStore{client: client}
}

func (store *cloudWatchCapacityStore) PreviousUsage(
	ctx context.Context,
	now time.Time,
	names []string,
) (map[string]historicalUsage, error) {
	if store == nil || store.client == nil || now.IsZero() || len(names) == 0 {
		return nil, errors.New("the CloudWatch capacity history request is incomplete")
	}
	ordered := append([]string(nil), names...)
	sort.Strings(ordered)
	queries := make([]cloudwatchtypes.MetricDataQuery, 0, len(ordered))
	queryNames := make(map[string]string, len(ordered))
	for index, name := range ordered {
		if name != "log_ingestion" && name != "trace_ingestion" {
			return nil, errors.New("the CloudWatch capacity history limit is invalid")
		}
		if index > 0 && ordered[index-1] == name {
			return nil, errors.New("the CloudWatch capacity history limit is duplicated")
		}
		id := "q" + strconv.Itoa(index)
		queryNames[id] = name
		queries = append(queries, cloudwatchtypes.MetricDataQuery{
			Id: aws.String(id),
			MetricStat: &cloudwatchtypes.MetricStat{
				Metric: &cloudwatchtypes.Metric{
					Namespace:  aws.String(capacityNamespace),
					MetricName: aws.String("CurrentUsage"),
					Dimensions: capacityDimensions(name, ""),
				},
				Period: aws.Int32(900),
				Stat:   aws.String("Maximum"),
			},
			ReturnData: aws.Bool(true),
		})
	}
	start := now.Add(-capacityHistoryWindow)
	output, err := store.client.GetMetricData(ctx, &cloudwatch.GetMetricDataInput{
		StartTime:         aws.Time(start),
		EndTime:           aws.Time(now),
		MetricDataQueries: queries,
		MaxDatapoints:     aws.Int32(10_000),
		ScanBy:            cloudwatchtypes.ScanByTimestampAscending,
	})
	if err != nil {
		return nil, err
	}
	if output == nil || len(output.Messages) != 0 || aws.ToString(output.NextToken) != "" || len(output.MetricDataResults) != len(queries) {
		return nil, errors.New("the CloudWatch capacity history response is incomplete")
	}
	usage := make(map[string]historicalUsage, len(queries))
	for _, result := range output.MetricDataResults {
		id := aws.ToString(result.Id)
		name, expected := queryNames[id]
		if !expected {
			return nil, errors.New("the CloudWatch capacity history response has an unknown query")
		}
		if _, duplicate := usage[name]; duplicate || result.StatusCode != cloudwatchtypes.StatusCodeComplete ||
			len(result.Messages) != 0 || len(result.Timestamps) != len(result.Values) {
			return nil, errors.New("the CloudWatch capacity history result is incomplete")
		}
		if len(result.Values) == 0 {
			usage[name] = historicalUsage{}
			continue
		}
		if result.Timestamps[0].Before(start) || !result.Timestamps[0].Before(now) {
			return nil, errors.New("the CloudWatch capacity history timestamp is invalid")
		}
		for index := 1; index < len(result.Timestamps); index++ {
			if !result.Timestamps[index-1].Before(result.Timestamps[index]) {
				return nil, errors.New("the CloudWatch capacity history timestamps are unordered")
			}
		}
		value, err := parseExactNumber(strconv.FormatFloat(result.Values[0], 'g', -1, 64))
		if err != nil {
			return nil, errors.New("the CloudWatch capacity history value is invalid")
		}
		usage[name] = historicalUsage{Value: value, SampledAt: result.Timestamps[0], Present: true}
	}
	return usage, nil
}

func (store *cloudWatchCapacityStore) Publish(ctx context.Context, metrics []capacityMetric) error {
	if store == nil || store.client == nil || len(metrics) == 0 || len(metrics) > 1000 {
		return errors.New("the CloudWatch capacity metric batch is incomplete")
	}
	sampledAt := metrics[0].SampledAt
	if sampledAt.IsZero() {
		return errors.New("the CloudWatch capacity metric timestamp is invalid")
	}
	seen := make(map[string]struct{}, len(metrics))
	data := make([]cloudwatchtypes.MetricDatum, 0, len(metrics))
	for _, metric := range metrics {
		contract, known := capacityMetricContracts[metric.Name]
		if !known || !metric.SampledAt.Equal(sampledAt) || metric.Value.Sign() < 0 ||
			contract.limit != (metric.Limit != "") || contract.environment != (metric.Environment != "") {
			return errors.New("the CloudWatch capacity metric is invalid")
		}
		if metric.Limit != "" {
			if _, known := freeCapacityLimits[metric.Limit]; !known {
				return errors.New("the CloudWatch capacity limit is invalid")
			}
		}
		if metric.Environment != "" {
			if _, known := environmentCapacityAllocations[metric.Environment]; !known {
				return errors.New("the CloudWatch capacity environment is invalid")
			}
		}
		identity := metric.Name + "\x00" + metric.Limit + "\x00" + metric.Environment
		if _, duplicate := seen[identity]; duplicate {
			return errors.New("the CloudWatch capacity metric is duplicated")
		}
		seen[identity] = struct{}{}
		value, err := strconv.ParseFloat(metric.Value.String(), 64)
		if err != nil {
			return errors.New("the CloudWatch capacity metric value is invalid")
		}
		data = append(data, cloudwatchtypes.MetricDatum{
			MetricName: aws.String(metric.Name),
			Dimensions: capacityDimensions(metric.Limit, metric.Environment),
			Timestamp:  aws.Time(metric.SampledAt),
			Unit:       contract.unit,
			Value:      aws.Float64(value),
		})
	}
	output, err := store.client.PutMetricData(ctx, &cloudwatch.PutMetricDataInput{
		Namespace:  aws.String(capacityNamespace),
		MetricData: data,
	})
	if err != nil {
		return err
	}
	if output == nil {
		return errors.New("the CloudWatch capacity metric batch was not confirmed")
	}
	return nil
}

func capacityDimensions(limit, environment string) []cloudwatchtypes.Dimension {
	dimensions := make([]cloudwatchtypes.Dimension, 0, 2)
	if environment != "" {
		dimensions = append(dimensions, cloudwatchtypes.Dimension{Name: aws.String("Environment"), Value: aws.String(environment)})
	}
	if limit != "" {
		dimensions = append(dimensions, cloudwatchtypes.Dimension{Name: aws.String("Limit"), Value: aws.String(limit)})
	}
	dimensions = append(dimensions, cloudwatchtypes.Dimension{Name: aws.String("StackName"), Value: aws.String(telemetry.GrafanaStackSlug)})
	return dimensions
}
