package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/acctbl/accountable/internal/platform/clock"
	"github.com/acctbl/accountable/internal/platform/telemetry"
)

const capacityNamespace = "Accountable/GrafanaCapacity"

var freeCapacityLimits = map[string]exactNumber{
	"metric_series":   mustExactNumber("10000"),
	"log_ingestion":   mustExactNumber("50"),
	"trace_ingestion": mustExactNumber("50"),
	"grafana_users":   mustExactNumber("3"),
	"irm_users":       mustExactNumber("3"),
	"alert_rules":     mustExactNumber("500"),
	"alert_instances": mustExactNumber("1000"),
}

var environmentCapacityAllocations = map[string]exactNumber{
	"development": mustExactNumber("10"),
	"staging":     mustExactNumber("15"),
	"production":  mustExactNumber("25"),
}

type capacityMetricStore interface {
	PreviousUsage(context.Context, time.Time, []string) (map[string]historicalUsage, error)
	Publish(context.Context, []capacityMetric) error
}

type capacityProducer struct {
	deployment   deployment
	cloudAPI     *grafanaClient
	stackAPI     *grafanaClient
	metrics      capacityMetricStore
	clock        clock.Clock
	approvedPlan string
}

type capacityValue struct {
	Current       exactNumber
	Limit         exactNumber
	ReportedLimit exactNumber
}

type stackCapacity struct {
	Plan            string
	Trial           bool
	IncidentEnabled bool
	Values          map[string]capacityValue
}

type capacityMetric struct {
	Name        string
	Value       exactNumber
	SampledAt   time.Time
	Limit       string
	Environment string
}

type historicalUsage struct {
	Value     exactNumber
	SampledAt time.Time
	Present   bool
}

type grafanaStack struct {
	ID                        int64        `json:"id"`
	Slug                      string       `json:"slug"`
	Plan                      string       `json:"plan"`
	Status                    string       `json:"status"`
	Trial                     int          `json:"trial"`
	Incident                  int          `json:"incident"`
	BillingGrafanaActiveUsers *exactNumber `json:"billingGrafanaActiveUsers"`
	BillingOnCallActiveUsers  *exactNumber `json:"billingOnCallActiveUsers"`
	AlertCount                *exactNumber `json:"alertCnt"`
	MetricSeries              *exactNumber `json:"hmInstancePromCurrentActiveSeries"`
	MetricsStatus             string       `json:"hmInstancePromStatus"`
	LogsStatus                string       `json:"hlInstanceStatus"`
	AlertingStatus            string       `json:"amInstanceStatus"`
	TracesStatus              string       `json:"htInstanceStatus"`
}

type billedUsageRecord struct {
	StackID      int64        `json:"stackId"`
	Total        *exactNumber `json:"totalUsage"`
	ActiveSeries *exactNumber `json:"activeSeries"`
	Grafana      *exactNumber `json:"grafanaUsage"`
	OnCall       *exactNumber `json:"onCallUsage"`
}

type billedUsageDimension struct {
	DimensionID   string              `json:"dimensionId"`
	IncludedUsage *exactNumber        `json:"includedUsage"`
	Usages        []billedUsageRecord `json:"usages"`
}

type billedUsageResponse struct {
	Items []billedUsageDimension `json:"items"`
}

func newCapacityProducer(ctx context.Context, settings deployment, metrics capacityMetricStore) (*capacityProducer, error) {
	approvedPlan := strings.TrimSpace(settings.ApprovedPlan)
	if metrics == nil || approvedPlan == "" {
		return nil, errors.New("the capacity producer configuration is incomplete")
	}
	cloudAPI, err := newGrafanaClient(ctx, settings)
	if err != nil {
		return nil, err
	}
	stackSettings := settings
	stackSettings.GrafanaBaseURL = "https://" + telemetry.GrafanaStackSlug + ".grafana.net"
	stackSettings.TokenParameter = settings.StackTokenParameter
	stackAPI, err := newGrafanaClient(ctx, stackSettings)
	if err != nil {
		return nil, err
	}
	return &capacityProducer{
		deployment:   settings,
		cloudAPI:     cloudAPI,
		stackAPI:     stackAPI,
		metrics:      metrics,
		clock:        clock.System{},
		approvedPlan: approvedPlan,
	}, nil
}

func (producer *capacityProducer) handle(ctx context.Context) error {
	now := producer.clock.Now().UTC()
	capacity, err := producer.read(ctx, now)
	if err != nil {
		return producer.publishFailure(ctx, now)
	}
	previous, err := producer.metrics.PreviousUsage(ctx, now, []string{"log_ingestion", "trace_ingestion"})
	if err != nil {
		return producer.publishFailure(ctx, now)
	}
	compliant := capacity.Plan == producer.approvedPlan
	metrics := []capacityMetric{
		newCapacityMetric("CapacityReadSuccess", mustExactNumber("1"), now),
		newCapacityMetric("TierPolicyCompliant", exactBoolean(compliant), now),
		newCapacityMetric("CapacitySampleFresh", mustExactNumber("1"), now),
		newCapacityMetric("IRMIncludedAtNoCost", exactBoolean(capacity.irmIncludedAtNoCost(compliant)), now),
		newCapacityMetric("SharedAllocationPercent", mustExactNumber("50"), now),
	}
	for _, environment := range sortedCapacityEnvironments() {
		metric := newCapacityMetric("AllocatedCapacityPercent", environmentCapacityAllocations[environment], now)
		metric.Environment = environment
		metrics = append(metrics, metric)
	}
	for _, name := range sortedCapacityLimitNames() {
		value, present := capacity.Values[name]
		if !present {
			return producer.publishFailure(ctx, now)
		}
		forecast := value.Current
		if name == "log_ingestion" || name == "trace_ingestion" {
			forecast = forecastMonthlyUsage(now, value.Current, previous[name])
		}
		allocatedLimit, err := capacityPlanningLimit(name, value.Limit)
		if err != nil {
			return producer.publishFailure(ctx, now)
		}
		currentPercent, err := percentage(value.Current, allocatedLimit)
		if err != nil {
			return producer.publishFailure(ctx, now)
		}
		forecastPercent, err := percentage(forecast, allocatedLimit)
		if err != nil {
			return producer.publishFailure(ctx, now)
		}
		for _, metric := range []capacityMetric{
			newCapacityMetric("CurrentUsage", value.Current, now),
			newCapacityMetric("HardLimit", value.Limit, now),
			newCapacityMetric("CurrentUsagePercent", currentPercent, now),
			newCapacityMetric("ForecastUsagePercent", forecastPercent, now),
		} {
			metric.Limit = name
			metrics = append(metrics, metric)
		}
	}
	return producer.metrics.Publish(ctx, metrics)
}

func (producer *capacityProducer) read(ctx context.Context, now time.Time) (stackCapacity, error) {
	stack, err := producer.readStack(ctx)
	if err != nil {
		return stackCapacity{}, err
	}
	billedRaw, err := producer.cloudAPI.send(ctx, http.MethodGet, fmt.Sprintf(
		"/api/orgs/%s/billed-usage?month=%d&year=%d",
		url.PathEscape(producer.deployment.OrganizationSlug),
		int(now.Month()),
		now.Year(),
	), nil)
	if err != nil {
		return stackCapacity{}, err
	}
	values, err := billedUsage(billedRaw, stack.ID)
	if err != nil {
		return stackCapacity{}, err
	}
	values["metric_series"] = maximumCapacityValue(values["metric_series"], *stack.MetricSeries)
	values["grafana_users"] = maximumCapacityValue(values["grafana_users"], *stack.BillingGrafanaActiveUsers)
	values["irm_users"] = maximumCapacityValue(values["irm_users"], *stack.BillingOnCallActiveUsers)
	values["alert_rules"] = capacityValue{
		Current:       *stack.AlertCount,
		Limit:         freeCapacityLimits["alert_rules"],
		ReportedLimit: freeCapacityLimits["alert_rules"],
	}
	maximumInstances, err := producer.maximumAlertInstances(ctx, *stack.AlertCount)
	if err != nil {
		return stackCapacity{}, err
	}
	values["alert_instances"] = capacityValue{
		Current:       maximumInstances,
		Limit:         freeCapacityLimits["alert_instances"],
		ReportedLimit: freeCapacityLimits["alert_instances"],
	}
	return stackCapacity{
		Plan:            stack.Plan,
		Trial:           stack.Trial != 0,
		IncidentEnabled: stack.Incident != 0,
		Values:          values,
	}, nil
}

func (producer *capacityProducer) readStack(ctx context.Context) (grafanaStack, error) {
	raw, err := producer.cloudAPI.send(
		ctx,
		http.MethodGet,
		"/api/orgs/"+url.PathEscape(producer.deployment.OrganizationSlug)+"/instances",
		nil,
	)
	if err != nil {
		return grafanaStack{}, err
	}
	var response struct {
		Items []grafanaStack `json:"items"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return grafanaStack{}, err
	}
	for _, stack := range response.Items {
		if stack.Slug != telemetry.GrafanaStackSlug {
			continue
		}
		if err := validateGrafanaStack(stack); err != nil {
			return grafanaStack{}, err
		}
		return stack, nil
	}
	return grafanaStack{}, errors.New("the shared Grafana stack is absent")
}

func validateGrafanaStack(stack grafanaStack) error {
	if stack.ID <= 0 || stack.Plan == "" || stack.BillingGrafanaActiveUsers == nil ||
		stack.BillingOnCallActiveUsers == nil || stack.AlertCount == nil || stack.MetricSeries == nil {
		return errors.New("the shared Grafana stack is incomplete")
	}
	for _, status := range []string{stack.Status, stack.MetricsStatus, stack.LogsStatus, stack.AlertingStatus, stack.TracesStatus} {
		if status != "active" {
			return errors.New("a required Grafana backend is inactive")
		}
	}
	return nil
}

func billedUsage(raw []byte, stackID int64) (map[string]capacityValue, error) {
	var response billedUsageResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	values := make(map[string]capacityValue, 5)
	for _, item := range response.Items {
		if item.IncludedUsage == nil {
			continue
		}
		for _, usage := range item.Usages {
			if usage.StackID != stackID {
				continue
			}
			if err := recordBilledUsage(values, item.DimensionID, *item.IncludedUsage, usage); err != nil {
				return nil, err
			}
		}
	}
	for _, name := range []string{"metric_series", "log_ingestion", "trace_ingestion", "grafana_users", "irm_users"} {
		if _, present := values[name]; !present {
			return nil, errors.New("grafana billed usage is incomplete")
		}
	}
	return values, nil
}

func recordBilledUsage(
	values map[string]capacityValue,
	dimension string,
	reportedLimit exactNumber,
	usage billedUsageRecord,
) error {
	set := func(name string, current *exactNumber) error {
		if current == nil {
			return errors.New("grafana billed usage is incomplete")
		}
		values[name] = capacityValue{
			Current:       *current,
			Limit:         minimumExact(reportedLimit, freeCapacityLimits[name]),
			ReportedLimit: reportedLimit,
		}
		return nil
	}
	switch dimension {
	case "hm":
		if usage.ActiveSeries != nil {
			return set("metric_series", usage.ActiveSeries)
		}
		return set("metric_series", usage.Total)
	case "hl":
		return set("log_ingestion", usage.Total)
	case "ht":
		return set("trace_ingestion", usage.Total)
	case "hg":
		if err := set("grafana_users", usage.Grafana); err != nil {
			return err
		}
		return set("irm_users", usage.OnCall)
	default:
		return nil
	}
}

func maximumCapacityValue(value capacityValue, candidate exactNumber) capacityValue {
	value.Current = maximumExact(value.Current, candidate)
	return value
}

func (producer *capacityProducer) maximumAlertInstances(ctx context.Context, ruleCount exactNumber) (exactNumber, error) {
	raw, err := producer.stackAPI.send(ctx, http.MethodGet, "/api/ruler/grafana/api/v1/rules", nil)
	if err != nil {
		return exactNumber{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return exactNumber{}, err
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return exactNumber{}, err
	}
	maximum, found := maximumAlertsArray(value)
	if ruleCount.Sign() > 0 && !found {
		return exactNumber{}, errors.New("grafana alert instances are incomplete")
	}
	return mustExactNumber(strconv.Itoa(maximum)), nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("the Grafana response contains trailing JSON")
}

func maximumAlertsArray(value any) (int, bool) {
	maximum, found := 0, false
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "alerts" {
				if alerts, ok := child.([]any); ok {
					found = true
					maximum = max(maximum, len(alerts))
				}
			}
			childMaximum, childFound := maximumAlertsArray(child)
			maximum, found = max(maximum, childMaximum), found || childFound
		}
	case []any:
		for _, child := range typed {
			childMaximum, childFound := maximumAlertsArray(child)
			maximum, found = max(maximum, childMaximum), found || childFound
		}
	}
	return maximum, found
}

func forecastMonthlyUsage(now time.Time, current exactNumber, previous historicalUsage) exactNumber {
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	if !previous.Present || previous.SampledAt.Before(monthStart) || previous.Value.Compare(current) > 0 || !previous.SampledAt.Before(now) {
		previous = historicalUsage{Value: mustExactNumber("0"), SampledAt: monthStart, Present: true}
	}
	elapsed := now.Sub(previous.SampledAt)
	if elapsed <= 0 {
		return current
	}
	nextMonth := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	remaining := nextMonth.Sub(now)
	growth := current.Subtract(previous.Value)
	projected, err := growth.Multiply(exactInteger(int64(remaining))).Divide(exactInteger(int64(elapsed)))
	if err != nil {
		return current
	}
	return current.Add(projected)
}

func capacityPlanningLimit(name string, hardLimit exactNumber) (exactNumber, error) {
	switch name {
	case "metric_series", "log_ingestion", "trace_ingestion", "alert_rules":
		return percentageOf(hardLimit, mustExactNumber("50"))
	case "grafana_users", "irm_users", "alert_instances":
		return hardLimit, nil
	default:
		return exactNumber{}, errors.New("the capacity limit is unknown")
	}
}

func (capacity stackCapacity) irmIncludedAtNoCost(tierCompliant bool) bool {
	value, present := capacity.Values["irm_users"]
	return tierCompliant && !capacity.Trial && capacity.IncidentEnabled && present &&
		value.ReportedLimit.Compare(freeCapacityLimits["irm_users"]) >= 0
}

func (producer *capacityProducer) publishFailure(ctx context.Context, now time.Time) error {
	return producer.metrics.Publish(ctx, []capacityMetric{newCapacityMetric("CapacityReadSuccess", mustExactNumber("0"), now)})
}

func newCapacityMetric(name string, value exactNumber, sampledAt time.Time) capacityMetric {
	return capacityMetric{Name: name, Value: value, SampledAt: sampledAt}
}

func percentage(value, limit exactNumber) (exactNumber, error) {
	if limit.Sign() <= 0 {
		return exactNumber{}, errors.New("the capacity limit must be positive")
	}
	return value.Multiply(mustExactNumber("100")).Divide(limit)
}

func percentageOf(value, percent exactNumber) (exactNumber, error) {
	return value.Multiply(percent).Divide(mustExactNumber("100"))
}

func exactBoolean(value bool) exactNumber {
	if value {
		return mustExactNumber("1")
	}
	return mustExactNumber("0")
}

func exactInteger(value int64) exactNumber {
	return mustExactNumber(strconv.FormatInt(value, 10))
}

func sortedCapacityLimitNames() []string {
	return []string{"alert_instances", "alert_rules", "grafana_users", "irm_users", "log_ingestion", "metric_series", "trace_ingestion"}
}

func sortedCapacityEnvironments() []string {
	return []string{"development", "staging", "production"}
}
