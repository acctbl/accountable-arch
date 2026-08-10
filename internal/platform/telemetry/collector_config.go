package telemetry

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

var collectorTaskIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type CollectorDeploymentConfig struct {
	SchemaVersion           int    `json:"schema_version"`
	Environment             string `json:"environment"`
	CellID                  string `json:"cell_id"`
	CollectorTaskID         string `json:"collector_task_id"`
	GrafanaOTLPEndpoint     string `json:"grafana_otlp_endpoint"`
	GrafanaTokenFile        string `json:"grafana_token_file"`
	ApplicationEndpoint     string `json:"application_endpoint"`
	SLIEndpoint             string `json:"sli_endpoint"`
	InternalMetricsEndpoint string `json:"internal_metrics_endpoint"`
	Protocol                string `json:"protocol"`
	SamplingPolicy          string `json:"sampling_policy"`
	RedactionPolicyVersion  string `json:"redaction_policy_version"`
}

func ParseCollectorDeploymentConfig(raw []byte) (CollectorDeploymentConfig, error) {
	var config CollectorDeploymentConfig
	if err := decodeStrictJSON(raw, &config); err != nil {
		return CollectorDeploymentConfig{}, err
	}
	if err := ValidateCollectorDeploymentConfig(config); err != nil {
		return CollectorDeploymentConfig{}, err
	}
	return config, nil
}

func ValidateCollectorDeploymentConfig(config CollectorDeploymentConfig) error {
	if config.SchemaVersion != ContractSchemaVersion ||
		!contains(registeredEnvironments, config.Environment) ||
		!cellIDPattern.MatchString(config.CellID) ||
		!collectorTaskIDPattern.MatchString(config.CollectorTaskID) {
		return contractError("collector identity")
	}
	endpoint, err := url.Parse(config.GrafanaOTLPEndpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return contractError("grafana_otlp_endpoint")
	}
	if !filepath.IsAbs(config.GrafanaTokenFile) || !strings.HasPrefix(filepath.Clean(config.GrafanaTokenFile), "/run/accountable/") {
		return contractError("grafana_token_file")
	}
	if config.ApplicationEndpoint != "0.0.0.0:4318" ||
		config.SLIEndpoint != "0.0.0.0:4319" ||
		config.InternalMetricsEndpoint != "127.0.0.1:8888" {
		return contractError("collector listener")
	}
	if config.Protocol != TelemetryProtocol || config.SamplingPolicy != TelemetrySamplingPolicy || config.RedactionPolicyVersion != TelemetryRedactionPolicy {
		return contractError("collector policy")
	}
	return nil
}

func BuildCollectorConfig(config CollectorDeploymentConfig) (string, error) {
	if err := ValidateCollectorDeploymentConfig(config); err != nil {
		return "", err
	}
	return fmt.Sprintf(`extensions:
  bearertokenauth/grafana:
    filename: %s
receivers:
  otlp/app:
    protocols:
      http:
        endpoint: %s
  otlp/slo:
    protocols:
      http:
        endpoint: %s
  prometheus/internal:
    config:
      scrape_configs:
        - job_name: collector
          static_configs:
            - targets: [%q]
processors:
  memory_limiter:
    check_interval: 1s
    limit_percentage: 75
    spike_limit_percentage: 15
  batch:
exporters:
  accountable_delivery/grafana:
    endpoint: %s
    auth:
      authenticator: bearertokenauth/grafana
    environment: %s
    cell_id: %s
    collector_task_id: %s
    retry_on_failure:
      enabled: true
      max_elapsed_time: 60s
    sending_queue:
      enabled: true
      sizer: items
      queue_size: 10000
  awsemf/slo:
    namespace: Accountable/SLO
    dimension_rollup_option: NoDimensionRollup
    sending_queue:
      enabled: false
    retry_on_failure:
      enabled: false
  awsemf/internal:
    namespace: Accountable/Collector
    dimension_rollup_option: NoDimensionRollup
service:
  extensions: [bearertokenauth/grafana]
  pipelines:
    traces/app:
      receivers: [otlp/app]
      processors: [memory_limiter, batch]
      exporters: [accountable_delivery/grafana]
    metrics/app:
      receivers: [otlp/app]
      processors: [memory_limiter, batch]
      exporters: [accountable_delivery/grafana]
    logs/app:
      receivers: [otlp/app]
      processors: [memory_limiter, batch]
      exporters: [accountable_delivery/grafana]
    metrics/slo:
      receivers: [otlp/slo]
      processors: [memory_limiter]
      exporters: [awsemf/slo]
    metrics/internal:
      receivers: [prometheus/internal]
      processors: [memory_limiter, batch]
      exporters: [awsemf/internal]
`, config.GrafanaTokenFile, config.ApplicationEndpoint, config.SLIEndpoint, config.InternalMetricsEndpoint, config.GrafanaOTLPEndpoint, config.Environment, config.CellID, config.CollectorTaskID), nil
}
