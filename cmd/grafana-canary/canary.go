package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/acctbl/accountable/internal/platform/telemetry"
)

// canaryQuery is one required probe. A metrics query runs against the Grafana
// metrics data source; a CloudWatch query runs against the CloudWatch data
// source, which is not a Prometheus endpoint and answers only through the
// documented data-source query API.
type canaryQuery struct {
	Name string `json:"name"`
	// DataSource is the Grafana data-source name. A UID is generated per stack, so
	// naming the data source keeps this configuration readable and stable.
	DataSource     string   `json:"data_source"`
	Kind           string   `json:"kind"`
	Expression     string   `json:"expression"`
	Namespace      string   `json:"namespace"`
	MetricName     string   `json:"metric_name"`
	Path           string   `json:"path"`
	ResultContract string   `json:"result_contract"`
	Expected       []string `json:"expected"`
}

const (
	queryKindMetrics    = "metrics"
	queryKindCloudWatch = "cloudwatch"
	queryKindPath       = "path"
	queryKindLogs       = "logs"
	queryKindTraces     = "traces"
)

type canary struct {
	deployment deployment
	grafana    *grafanaClient
}

type alertRuleHealth struct {
	Health    string `json:"health"`
	LastError string `json:"lastError"`
}

type alertRuleGroup struct {
	Rules []alertRuleHealth `json:"rules"`
}

type alertRuleResponse struct {
	Status string `json:"status"`
	Data   struct {
		Groups []alertRuleGroup `json:"groups"`
	} `json:"data"`
}

func newCanary(ctx context.Context, settings deployment) (*canary, error) {
	if len(settings.Queries) == 0 {
		return nil, errors.New("the canary deployment document lists no required query")
	}
	grafana, err := newGrafanaClient(ctx, settings)
	if err != nil {
		return nil, err
	}
	return &canary{deployment: settings, grafana: grafana}, nil
}

// handle queries Grafana from outside Grafana and publishes the result straight
// to CloudWatch, so the canary never depends on the alert translator.
func (c *canary) handle(ctx context.Context) error {
	var success int64 = 1
	for _, query := range c.deployment.Queries {
		if err := c.probe(ctx, query); err != nil {
			success = 0
			break
		}
	}
	return publishEMF(
		c.grafana.clock,
		telemetry.ObservabilityNamespace,
		map[string]string{
			"StackName":   c.deployment.StackName,
			"Environment": c.deployment.Environment,
		},
		[]emfMetric{{Name: "CanarySuccess", Unit: "None", Value: success}},
	)
}

func (c *canary) probe(ctx context.Context, query canaryQuery) error {
	if query.Kind == queryKindPath {
		raw, err := c.grafana.send(ctx, http.MethodGet, query.Path, nil)
		if err != nil {
			return fmt.Errorf("the canary query %s failed: %w", query.Name, err)
		}
		return requirePathResult(query, raw)
	}
	target, err := c.target(ctx, query)
	if err != nil {
		return err
	}
	raw, err := c.grafana.query(ctx, target)
	if err != nil {
		return fmt.Errorf("the canary query %s failed: %w", query.Name, err)
	}
	return requireCurrentFrame(query, raw)
}

func requirePathResult(query canaryQuery, raw []byte) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return fmt.Errorf("the canary query %s has no result", query.Name)
	}
	switch query.ResultContract {
	case "dashboard-set":
		return requireDashboardSet(query, raw)
	case "healthy-alert-rules":
		return requireHealthyAlertRules(query, raw)
	default:
		return fmt.Errorf("the canary query %s has no result contract", query.Name)
	}
}

func requireDashboardSet(query canaryQuery, raw []byte) error {
	var results []struct {
		UID  string `json:"uid"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &results); err != nil || len(results) != len(query.Expected) {
		return fmt.Errorf("the canary query %s has an incomplete dashboard set", query.Name)
	}
	found := make(map[string]bool, len(results))
	for _, result := range results {
		if result.UID == "" || result.Type != "dash-db" {
			return fmt.Errorf("the canary query %s has an invalid dashboard", query.Name)
		}
		found[result.UID] = true
	}
	for _, uid := range query.Expected {
		if !found[uid] {
			return fmt.Errorf("the canary query %s is missing dashboard %s", query.Name, uid)
		}
	}
	return nil
}

func requireHealthyAlertRules(query canaryQuery, raw []byte) error {
	var response alertRuleResponse
	if err := json.Unmarshal(raw, &response); err != nil || response.Status != "success" {
		return fmt.Errorf("the canary query %s returned an invalid rule response", query.Name)
	}
	ruleCount := 0
	for _, group := range response.Data.Groups {
		for _, rule := range group.Rules {
			ruleCount++
			if rule.Health != "ok" || rule.LastError != "" {
				return fmt.Errorf("the canary query %s found an unhealthy rule", query.Name)
			}
		}
	}
	if ruleCount == 0 {
		return fmt.Errorf("the canary query %s found no alert rule", query.Name)
	}
	return nil
}

// target builds the per-type query payload. CloudWatch is not a Prometheus
// endpoint, so it answers only through the data-source query API.
func (c *canary) target(ctx context.Context, query canaryQuery) (map[string]any, error) {
	uid, err := c.grafana.dataSourceUID(ctx, query.DataSource)
	if err != nil {
		return nil, fmt.Errorf("the canary query %s cannot resolve data source %s: %w", query.Name, query.DataSource, err)
	}
	target := map[string]any{
		"refId":         "A",
		"datasource":    map[string]string{"uid": uid},
		"intervalMs":    60000,
		"maxDataPoints": 1,
	}
	switch query.Kind {
	case queryKindMetrics:
		target["expr"] = query.Expression
	case queryKindLogs:
		target["expr"] = query.Expression
		target["queryType"] = "range"
		target["editorMode"] = "code"
		target["maxLines"] = 1
	case queryKindTraces:
		target["query"] = query.Expression
		target["queryType"] = "traceqlSearch"
		target["tableType"] = "traces"
	case queryKindCloudWatch:
		target["queryMode"] = "Metrics"
		target["namespace"] = query.Namespace
		target["metricName"] = query.MetricName
		target["statistic"] = "Sum"
		target["period"] = "300"
		target["region"] = "default"
		target["metricQueryType"] = 0
		target["metricEditorMode"] = 0
	default:
		return nil, fmt.Errorf("the canary query %s has an unknown kind", query.Name)
	}
	return target, nil
}

// requireCurrentFrame fails the canary when a required query answers with no frame.
func requireCurrentFrame(query canaryQuery, raw []byte) error {
	var response struct {
		Results map[string]struct {
			Error  string `json:"error"`
			Frames []struct {
				Data struct {
					Values [][]json.RawMessage `json:"values"`
				} `json:"data"`
			} `json:"frames"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return fmt.Errorf("the canary query %s returned an unreadable result", query.Name)
	}
	if len(response.Results) == 0 {
		return fmt.Errorf("the canary query %s has no result", query.Name)
	}
	found := false
	for _, result := range response.Results {
		if result.Error != "" {
			return fmt.Errorf("the canary query %s failed", query.Name)
		}
		for _, frame := range result.Frames {
			if len(frame.Data.Values) < 2 {
				continue
			}
			for _, value := range frame.Data.Values[len(frame.Data.Values)-1] {
				if len(value) > 0 && string(value) != "null" {
					switch query.ResultContract {
					case "":
						return nil
					case "zero":
						number, err := parseExactNumber(string(value))
						if err != nil || number.Sign() != 0 {
							return fmt.Errorf("the canary query %s is not zero", query.Name)
						}
						found = true
					default:
						return fmt.Errorf("the canary query %s has no result contract", query.Name)
					}
				}
			}
		}
	}
	if found {
		return nil
	}
	return fmt.Errorf("the canary query %s has no current result", query.Name)
}
