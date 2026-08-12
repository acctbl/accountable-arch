package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/acctbl/accountable/internal/platform/clock"
)

func TestRequireCurrentFrameFailsWithoutData(t *testing.T) {
	t.Parallel()

	query := canaryQuery{Name: "metrics_reachable"}
	tests := map[string]struct {
		body     string
		accepted bool
	}{
		"one value": {`{"results":{"A":{"frames":[{"data":{"values":[[1762000000000],[1]]}}]}}}`, true},
		// A timestamp column with a null value is the shape a stale series
		// returns, and it must never read as green.
		"timestamp with null value": {`{"results":{"A":{"frames":[{"data":{"values":[[1762000000000],[null]]}}]}}}`, false},
		"timestamps only":           {`{"results":{"A":{"frames":[{"data":{"values":[[1762000000000]]}}]}}}`, false},
		"no results":                {`{"results":{}}`, false},
		"no frames":                 {`{"results":{"A":{"frames":[]}}}`, false},
		"empty columns":             {`{"results":{"A":{"frames":[{"data":{"values":[]}}]}}}`, false},
		"empty column":              {`{"results":{"A":{"frames":[{"data":{"values":[[]]}}]}}}`, false},
		"data source error":         {`{"results":{"A":{"error":"data source not found","frames":[]}}}`, false},
		"unreadable":                {`not json`, false},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := requireCurrentFrame(query, []byte(test.body))
			if test.accepted != (err == nil) {
				t.Fatalf("accepted = %t, err = %v", test.accepted, err)
			}
		})
	}
}

func TestRequireCurrentFrameCanRequireAnExactZero(t *testing.T) {
	t.Parallel()

	query := canaryQuery{Name: "critical_alerts_clear", ResultContract: "zero"}
	for name, test := range map[string]struct {
		body     string
		accepted bool
	}{
		"zero":     {`{"results":{"A":{"frames":[{"data":{"values":[[1762000000000],[0]]}}]}}}`, true},
		"firing":   {`{"results":{"A":{"frames":[{"data":{"values":[[1762000000000],[1]]}}]}}}`, false},
		"fraction": {`{"results":{"A":{"frames":[{"data":{"values":[[1762000000000],[0.1]]}}]}}}`, false},
		"null":     {`{"results":{"A":{"frames":[{"data":{"values":[[1762000000000],[null]]}}]}}}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			err := requireCurrentFrame(query, []byte(test.body))
			if test.accepted != (err == nil) {
				t.Fatalf("accepted = %t, err = %v", test.accepted, err)
			}
		})
	}
}

// The EMF document is the contract with CloudWatch: a wrong shape publishes
// nothing and fails silently, so its exact structure is asserted here.
func TestPublishEMFDocumentShape(t *testing.T) {
	instant := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	stdout := captureStdout(t)

	err := publishEMF(
		clock.Fixed{Instant: instant},
		"Accountable/Observability",
		map[string]string{"StackName": "acctbl", "Environment": "development"},
		[]emfMetric{{Name: "CanarySuccess", Unit: "None", Value: 1}},
	)
	if err != nil {
		t.Fatal(err)
	}

	var document map[string]any
	if err := json.Unmarshal([]byte(stdout()), &document); err != nil {
		t.Fatalf("the document is not JSON: %v", err)
	}
	if document["CanarySuccess"] != float64(1) {
		t.Fatalf("metric value = %v", document["CanarySuccess"])
	}
	if document["StackName"] != "acctbl" || document["Environment"] != "development" {
		t.Fatalf("dimension values = %v", document)
	}
	metadata, ok := document["_aws"].(map[string]any)
	if !ok {
		t.Fatal("the document has no _aws metadata")
	}
	if metadata["Timestamp"] != float64(instant.UnixMilli()) {
		t.Fatalf("timestamp = %v", metadata["Timestamp"])
	}
	directives, ok := metadata["CloudWatchMetrics"].([]any)
	if !ok || len(directives) != 1 {
		t.Fatalf("metric directives = %v", metadata["CloudWatchMetrics"])
	}
	directive := directives[0].(map[string]any)
	if directive["Namespace"] != "Accountable/Observability" {
		t.Fatalf("namespace = %v", directive["Namespace"])
	}
	// Dimension names are sorted so one metric never splits across two series.
	dimensions := directive["Dimensions"].([]any)[0].([]any)
	if len(dimensions) != 2 || dimensions[0] != "Environment" || dimensions[1] != "StackName" {
		t.Fatalf("dimensions = %v", dimensions)
	}
}

func TestPublishEMFWritesNothingWithoutMetrics(t *testing.T) {
	stdout := captureStdout(t)
	if err := publishEMF(clock.System{}, "Accountable/Observability", nil, nil); err != nil {
		t.Fatal(err)
	}
	if stdout() != "" {
		t.Fatalf("wrote %q", stdout())
	}
}

func TestPathProofRequiresEveryDashboardAndHealthyAlertRule(t *testing.T) {
	t.Parallel()

	dashboardQuery := canaryQuery{
		Name:           "dashboards_reachable",
		ResultContract: "dashboard-set",
		Expected:       []string{"identity", "budget"},
	}
	if err := requirePathResult(dashboardQuery, []byte(`[{"uid":"budget","type":"dash-db"},{"uid":"identity","type":"dash-db"}]`)); err != nil {
		t.Fatalf("complete dashboards: %v", err)
	}
	if err := requirePathResult(dashboardQuery, []byte(`[{"uid":"identity","type":"dash-db"}]`)); err == nil {
		t.Fatal("an incomplete dashboard set was accepted")
	}

	ruleQuery := canaryQuery{Name: "alert_rules_reachable", ResultContract: "healthy-alert-rules"}
	if err := requirePathResult(ruleQuery, []byte(`{"status":"success","data":{"groups":[{"rules":[{"name":"budget","health":"ok","lastError":""}]}]}}`)); err != nil {
		t.Fatalf("healthy alert rules: %v", err)
	}
	for name, body := range map[string]string{
		"no rules":         `{"status":"success","data":{"groups":[]}}`,
		"evaluation error": `{"status":"success","data":{"groups":[{"rules":[{"name":"budget","health":"err","lastError":"query failed"}]}]}}`,
		"failed request":   `{"status":"error","data":{"groups":[]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := requirePathResult(ruleQuery, []byte(body)); err == nil {
				t.Fatal("an unhealthy rule response was accepted")
			}
		})
	}
}
