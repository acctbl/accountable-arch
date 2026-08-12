package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout redirects the EMF writer for one test and reports what it wrote.
func captureStdout(t *testing.T) func() string {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	captured := make(chan string, 1)
	go func() {
		content, _ := io.ReadAll(reader)
		captured <- string(content)
	}()
	t.Cleanup(func() { os.Stdout = original })
	return func() string {
		_ = writer.Close()
		return strings.TrimSpace(<-captured)
	}
}

func TestReadDeploymentRefusesAnIncompleteDocument(t *testing.T) {
	directory := t.TempDir()
	complete := `{
		"schema_version": 1,
		"handler": "canary",
		"region": "eu-west-2",
		"environment": "development",
		"stack_name": "acctbl",
		"grafana_base_url": "https://acctbl.grafana.net",
		"token_parameter": "/accountable/observability/development/grafana-health-token",
		"queries": [{"name": "metrics_reachable", "kind": "metrics", "data_source": "grafanacloud-acctbl-prom", "expression": "vector(1)"}]
	}`
	path := directory + "/deployment.json"
	if err := os.WriteFile(path, []byte(complete), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readDeployment(path); err != nil {
		t.Fatalf("complete document: %v", err)
	}

	// A canary with no required query proves nothing, so it must not start.
	rejected := map[string]string{
		"no queries":     strings.Replace(complete, `"queries": [{"name": "metrics_reachable", "kind": "metrics", "data_source": "grafanacloud-acctbl-prom", "expression": "vector(1)"}]`, `"queries": []`, 1),
		"another stack":  strings.Replace(complete, `"acctbl"`, `"someone-else"`, 1),
		"no environment": strings.Replace(complete, `"environment": "development",`, `"environment": "",`, 1),
		"no token":       strings.Replace(complete, `"/accountable/observability/development/grafana-health-token"`, `""`, 1),
		"wrong schema":   strings.Replace(complete, `"schema_version": 1`, `"schema_version": 2`, 1),
	}
	for name, document := range rejected {
		t.Run(name, func(t *testing.T) {
			path := t.TempDir() + "/deployment.json"
			if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readDeployment(path); err == nil {
				t.Fatal("an incomplete document was accepted")
			}
		})
	}
}

func TestEveryEnvironmentCanaryProvesTheCompleteObservabilitySurface(t *testing.T) {
	t.Parallel()

	expected := map[string]bool{
		"metrics_current":       true,
		"logs_current":          true,
		"traces_current":        true,
		"cloudwatch_current":    true,
		"dashboards_reachable":  true,
		"alert_rules_reachable": true,
		"critical_alerts_clear": true,
	}
	for _, environment := range []string{"development", "staging", "production"} {
		environment := environment
		t.Run(environment, func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile("../../infra/observability/" + environment + "/canary.json")
			if err != nil {
				t.Fatal(err)
			}
			var settings deployment
			if err := json.Unmarshal(raw, &settings); err != nil {
				t.Fatal(err)
			}
			actual := make(map[string]bool, len(settings.Queries))
			for _, query := range settings.Queries {
				actual[query.Name] = true
			}
			if len(actual) != len(expected) {
				t.Fatalf("query count = %d, want %d", len(actual), len(expected))
			}
			for name := range expected {
				if !actual[name] {
					t.Fatalf("missing query %q", name)
				}
			}
		})
	}
}
