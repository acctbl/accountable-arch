package main

import (
	"testing"
	"time"
)

func TestBilledUsageKeepsExactValuesAndNeverWidensFreeLimits(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
  "items": [
    {"dimensionId":"hl","includedUsage":60,"usages":[{"stackId":42,"totalUsage":12.5}]},
    {"dimensionId":"ht","includedUsage":100,"usages":[{"stackId":42,"totalUsage":5.25}]},
    {"dimensionId":"hm","includedUsage":12000,"usages":[{"stackId":42,"totalUsage":9500.75,"activeSeries":9000}]},
    {"dimensionId":"hg","includedUsage":5,"usages":[{"stackId":42,"totalUsage":3,"grafanaUsage":2,"onCallUsage":1}]}
  ]
}`)

	usage, err := billedUsage(raw, 42)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]struct {
		current string
		limit   string
	}{
		"metric_series":   {current: "9000", limit: "10000"},
		"log_ingestion":   {current: "12.5", limit: "50"},
		"trace_ingestion": {current: "5.25", limit: "50"},
		"grafana_users":   {current: "2", limit: "3"},
		"irm_users":       {current: "1", limit: "3"},
	}
	for name, want := range expected {
		actual, present := usage[name]
		if !present {
			t.Fatalf("missing %s", name)
		}
		if actual.Current.String() != want.current || actual.Limit.String() != want.limit {
			t.Fatalf("%s = %s/%s, want %s/%s", name, actual.Current.String(), actual.Limit.String(), want.current, want.limit)
		}
	}
}

func TestMonthlyForecastUsesExactCalendarTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	current := mustExactNumber("7")
	previous := historicalUsage{Value: mustExactNumber("0"), SampledAt: now.Add(-7 * 24 * time.Hour), Present: true}
	forecast := forecastMonthlyUsage(now, current, previous)
	if forecast.String() != "28" {
		t.Fatalf("forecast = %s, want 28", forecast.String())
	}
}

func TestMonthlyForecastFallsBackToMonthToDateRateWithoutHistory(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	forecast := forecastMonthlyUsage(now, mustExactNumber("10"), historicalUsage{})
	if forecast.String() != "31" {
		t.Fatalf("forecast = %s, want 31", forecast.String())
	}
}

func TestCapacityPlanningLimitAllocatesOnlyDivisibleResources(t *testing.T) {
	t.Parallel()

	expected := map[string]string{
		"metric_series":   "5000",
		"log_ingestion":   "25",
		"trace_ingestion": "25",
		"alert_rules":     "250",
		"grafana_users":   "3",
		"irm_users":       "3",
		"alert_instances": "1000",
	}
	for name, want := range expected {
		limit, err := capacityPlanningLimit(name, freeCapacityLimits[name])
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if limit.String() != want {
			t.Fatalf("%s limit = %s, want %s", name, limit.String(), want)
		}
	}
}

func TestExactNumberAcceptsJSONDecimalsAndRejectsInvalidCapacity(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"0", "12.5", "1e3", "0.000001"} {
		parsed, err := parseExactNumber(value)
		if err != nil {
			t.Fatalf("parse %s: %v", value, err)
		}
		if parsed.Sign() < 0 {
			t.Fatalf("parse %s produced a negative value", value)
		}
	}
	for _, value := range []string{"-1", "NaN", "Inf", "", "1e1001"} {
		if _, err := parseExactNumber(value); err == nil {
			t.Fatalf("invalid capacity %q was accepted", value)
		}
	}
}
