package telemetry

import (
	"embed"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

//go:embed templates/collector.yaml.tmpl
var collectorTemplates embed.FS

var (
	collectorTaskIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	grafanaInstancePattern = regexp.MustCompile(`^[0-9]{1,20}$`)
	collectorRegionPattern = regexp.MustCompile(`^[a-z]{2}-[a-z]+-[0-9]+$`)
)

// scopeReservedKey removes every scope attribute. OTTL has no empty list literal,
// so the allow-list keeps one reserved key that no producer emits.
const scopeReservedKey = "accountable.reserved.none"

type CollectorDeploymentConfig struct {
	SchemaVersion       int    `json:"schema_version"`
	Environment         string `json:"environment"`
	CellID              string `json:"cell_id"`
	CollectorTaskID     string `json:"collector_task_id"`
	AWSRegion           string `json:"aws_region"`
	GrafanaOTLPEndpoint string `json:"grafana_otlp_endpoint"`
	// GrafanaInstanceID and GrafanaToken are the Basic credentials that Grafana
	// Cloud OTLP requires. They reach the Collector only through its own
	// configuration file, which collector-init writes with mode 0600.
	GrafanaInstanceID       string `json:"grafana_instance_id"`
	GrafanaToken            string `json:"-"`
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

// ValidateCollectorIdentity checks the trusted deployment identity that every
// Collector component binds to.
func ValidateCollectorIdentity(environment, cellID, collectorTaskID string) error {
	if !contains(registeredEnvironments, environment) ||
		!cellIDPattern.MatchString(cellID) ||
		!collectorTaskIDPattern.MatchString(collectorTaskID) {
		return contractError("collector identity")
	}
	return nil
}

func ValidateCollectorDeploymentConfig(config CollectorDeploymentConfig) error {
	if config.SchemaVersion != ContractSchemaVersion {
		return contractError("collector schema_version")
	}
	if err := ValidateCollectorIdentity(config.Environment, config.CellID, config.CollectorTaskID); err != nil {
		return err
	}
	if !collectorRegionPattern.MatchString(config.AWSRegion) {
		return contractError("aws_region")
	}
	endpoint, err := url.Parse(config.GrafanaOTLPEndpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return contractError("grafana_otlp_endpoint")
	}
	if !grafanaInstancePattern.MatchString(config.GrafanaInstanceID) {
		return contractError("grafana_instance_id")
	}
	if config.ApplicationEndpoint != "0.0.0.0:"+strconv.Itoa(CollectorApplicationPort) ||
		config.SLIEndpoint != "0.0.0.0:"+strconv.Itoa(CollectorSLIPort) ||
		config.InternalMetricsEndpoint != "127.0.0.1:"+strconv.Itoa(CollectorInternalPort) {
		return contractError("collector listener")
	}
	if config.Protocol != TelemetryProtocol || config.SamplingPolicy != TelemetrySamplingPolicy || config.RedactionPolicyVersion != TelemetryRedactionPolicy {
		return contractError("collector policy")
	}
	return nil
}

// CollectorLogGroups reports the EMF log groups that one cell Collector writes.
func CollectorLogGroups(cellID string) (sli string, internal string, err error) {
	if !cellIDPattern.MatchString(cellID) {
		return "", "", contractError("accountable.cell.id")
	}
	return "/accountable/" + cellID + "/emf-sli", "/accountable/" + cellID + "/emf-internal", nil
}

func BuildCollectorConfig(config CollectorDeploymentConfig) (string, error) {
	if err := ValidateCollectorDeploymentConfig(config); err != nil {
		return "", err
	}
	if config.GrafanaToken == "" {
		return "", contractError("grafana token")
	}
	values, err := collectorTemplateValues(config)
	if err != nil {
		return "", err
	}
	parsed, err := template.New("collector.yaml.tmpl").
		Funcs(template.FuncMap{"q": singleQuotedYAML}).
		ParseFS(collectorTemplates, "templates/collector.yaml.tmpl")
	if err != nil {
		return "", err
	}
	var rendered strings.Builder
	if err := parsed.Execute(&rendered, values); err != nil {
		return "", err
	}
	return rendered.String(), nil
}

type collectorTemplateData struct {
	CollectorDeploymentConfig
	InternalMetricsHost           string
	InternalMetricsPort           string
	GrafanaInstanceID             string
	GrafanaToken                  string
	SLILogGroup                   string
	InternalLogGroup              string
	SLONamespace                  string
	CollectorNamespace            string
	IdentityConflictConditions    []string
	TrustedIdentityStatements     []string
	ResourceKeepKeys              string
	ScopeKeepKeys                 string
	SpanKeepKeys                  string
	LogKeepKeys                   string
	MetricKeepKeys                string
	RuntimeIdentityCopyStatements []string
	SLIMetricRejections           []string
	SLIDatapointRejections        []string
	SLIRenameStatements           []string
	SLIDimensionStatements        []string
	SLIIndicatorMetrics           string
	SLIHeartbeatMetric            string
	InternalMetricRejection       string
	InternalDimensionStatements   []string
	InternalRenameStatements      []string
	DeliveryCounterMetrics        string
	DeliveryGaugeMetrics          string
	CollectorHealthMetrics        string
}

func collectorTemplateValues(config CollectorDeploymentConfig) (collectorTemplateData, error) {
	host, port, err := net.SplitHostPort(config.InternalMetricsEndpoint)
	if err != nil {
		return collectorTemplateData{}, contractError("collector listener")
	}
	sliLogGroup, internalLogGroup, err := CollectorLogGroups(config.CellID)
	if err != nil {
		return collectorTemplateData{}, err
	}
	spanAllowList, err := RegisteredSignalAttributes("trace")
	if err != nil {
		return collectorTemplateData{}, err
	}
	logAllowList, err := RegisteredSignalAttributes("log")
	if err != nil {
		return collectorTemplateData{}, err
	}
	metricAllowList, err := RegisteredSignalAttributes("metric")
	if err != nil {
		return collectorTemplateData{}, err
	}
	return collectorTemplateData{
		CollectorDeploymentConfig:     config,
		InternalMetricsHost:           host,
		InternalMetricsPort:           port,
		GrafanaInstanceID:             config.GrafanaInstanceID,
		GrafanaToken:                  config.GrafanaToken,
		SLILogGroup:                   sliLogGroup,
		InternalLogGroup:              internalLogGroup,
		SLONamespace:                  SLONamespace,
		CollectorNamespace:            CollectorNamespace,
		IdentityConflictConditions:    identityConflictConditions(config),
		TrustedIdentityStatements:     trustedIdentityStatements(config),
		ResourceKeepKeys:              keepKeys(RegisteredResourceAttributes),
		ScopeKeepKeys:                 keepKeys([]string{scopeReservedKey}),
		SpanKeepKeys:                  keepKeys(spanAllowList),
		LogKeepKeys:                   keepKeys(logAllowList),
		MetricKeepKeys:                keepKeys(metricAllowList),
		RuntimeIdentityCopyStatements: runtimeIdentityCopyStatements(),
		SLIMetricRejections:           sliMetricRejections(),
		SLIDatapointRejections:        []string{"Len(attributes) > 0", "value_int < 0", "value_double < 0"},
		SLIRenameStatements:           renameStatements(sliMetricMappings),
		SLIDimensionStatements:        sliDimensionStatements(config),
		SLIIndicatorMetrics:           strings.Join(sliIndicatorMetrics(), ", "),
		SLIHeartbeatMetric:            sliHeartbeatMetric,
		InternalMetricRejection:       nameRejection(internalSourceNames()),
		InternalDimensionStatements:   internalDimensionStatements(config),
		InternalRenameStatements:      internalRenameStatements(),
		DeliveryCounterMetrics:        "AcceptedItemCount, RefusedItemCount, SentItemCount, PermanentlyDroppedItemCount",
		DeliveryGaugeMetrics:          "PendingItemCount, OldestPendingItemAgeSeconds",
		CollectorHealthMetrics:        "CollectorUptimeSeconds, CollectorMemoryRssBytes",
	}, nil
}

func identityConflictConditions(config CollectorDeploymentConfig) []string {
	return []string{
		resourceConflict("deployment.environment.name", config.Environment),
		resourceConflict("accountable.cell.id", config.CellID),
	}
}

func resourceConflict(attribute, trusted string) string {
	return fmt.Sprintf(
		`resource.attributes[%q] != nil and resource.attributes[%q] != %q`,
		attribute, attribute, trusted,
	)
}

func trustedIdentityStatements(config CollectorDeploymentConfig) []string {
	return []string{
		fmt.Sprintf(`set(attributes["deployment.environment.name"], %q)`, config.Environment),
		fmt.Sprintf(`set(attributes["accountable.cell.id"], %q)`, config.CellID),
	}
}

func keepKeys(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, strconv.Quote(name))
	}
	return "keep_keys(attributes, [" + strings.Join(quoted, ", ") + "])"
}

func runtimeIdentityCopyStatements() []string {
	statements := make([]string, 0, len(RegisteredRuntimeIdentityAttributes))
	for _, name := range RegisteredRuntimeIdentityAttributes {
		statements = append(statements, fmt.Sprintf(
			`set(attributes[%q], resource.attributes[%q]) where resource.attributes[%q] != nil`,
			name, name, name,
		))
	}
	return statements
}

// OTTL reports the metric type as the pdata enum. Only a Sum can carry the
// monotonic delta points that the SLI pipeline accepts.
const ottlSumMetricType = 2

// OTTL reports delta temporality as the pdata enum value 1.
const ottlDeltaTemporality = 1

func sliMetricRejections() []string {
	return []string{
		nameRejection(sliSourceNames()),
		fmt.Sprintf("type != %d", ottlSumMetricType),
		fmt.Sprintf("aggregation_temporality != %d", ottlDeltaTemporality),
		"is_monotonic != true",
	}
}

func sliSourceNames() []string {
	names := make([]string, 0, len(sliMetricMappings))
	for _, mapping := range sliMetricMappings {
		names = append(names, mapping.Source)
	}
	return names
}

func sliIndicatorMetrics() []string {
	metrics := make([]string, 0, len(sliMetricMappings)-1)
	for _, mapping := range sliMetricMappings {
		if mapping.Metric != sliHeartbeatMetric {
			metrics = append(metrics, mapping.Metric)
		}
	}
	return metrics
}

func nameRejection(names []string) string {
	conditions := make([]string, 0, len(names))
	for _, name := range names {
		conditions = append(conditions, fmt.Sprintf("name != %q", name))
	}
	return strings.Join(conditions, " and ")
}

func renameStatements(mappings []cloudWatchMapping) []string {
	statements := make([]string, 0, len(mappings))
	for _, mapping := range mappings {
		statements = append(statements, fmt.Sprintf(
			"set(name, %q) where name == %q", mapping.Metric, mapping.Source,
		))
	}
	return statements
}

func sliDimensionStatements(config CollectorDeploymentConfig) []string {
	return []string{
		fmt.Sprintf(`set(attributes["Environment"], %q)`, config.Environment),
		fmt.Sprintf(`set(attributes["ServiceName"], %q)`, runtimeServices["api"]),
		fmt.Sprintf(
			`set(attributes["CellId"], %q) where metric.name == %q`,
			config.CellID, sliHeartbeatMetric,
		),
	}
}

func internalDimensionStatements(config CollectorDeploymentConfig) []string {
	statements := make([]string, 0, len(refusedItemMappings)*2+len(collectorHealthMetrics)+4)
	for _, mapping := range refusedItemMappings {
		match := fmt.Sprintf(
			`metric.name == %q and attributes["receiver"] == %q`,
			mapping.Source, mapping.Receiver,
		)
		statements = append(statements,
			fmt.Sprintf(`set(attributes["Pipeline"], %q) where %s`, mapping.Pipeline, match),
			fmt.Sprintf(`set(attributes["Signal"], %q) where %s`, mapping.Signal, match),
		)
	}
	refused := fmt.Sprintf(`IsMatch(metric.name, %q)`, "^"+refusedSourcePrefix)
	health := healthMetricCondition()
	statements = append(statements,
		fmt.Sprintf(`set(attributes["Environment"], %q) where %s`, config.Environment, refused),
		fmt.Sprintf(`set(attributes["CellId"], %q) where %s`, config.CellID, refused),
		`delete_key(attributes, "receiver")`,
		`delete_key(attributes, "transport")`,
		fmt.Sprintf(`set(attributes["Environment"], %q) where %s`, config.Environment, health),
		fmt.Sprintf(`set(attributes["CellId"], %q) where %s`, config.CellID, health),
		fmt.Sprintf(`set(attributes["CollectorTaskId"], %q) where %s`, config.CollectorTaskID, health),
	)
	return statements
}

func healthMetricCondition() string {
	conditions := make([]string, 0, len(collectorHealthMetrics))
	for _, name := range collectorHealthMetrics {
		conditions = append(conditions, fmt.Sprintf("metric.name == %q", name))
	}
	return strings.Join(conditions, " or ")
}

func internalRenameStatements() []string {
	statements := renameStatements(internalMetricMappings)
	return append(statements, fmt.Sprintf(
		`set(name, "RefusedItemCount") where IsMatch(name, %q)`, "^"+refusedSourcePrefix,
	))
}

func singleQuotedYAML(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
