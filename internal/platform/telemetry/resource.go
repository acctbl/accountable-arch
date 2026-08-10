package telemetry

var runtimeServices = map[string]string{
	"api":       "accountable-api",
	"bootstrap": "accountable-bootstrap",
	"migrate":   "accountable-migrate",
	"preflight": "accountable-preflight",
	"collector": "accountable-collector",
}

type ProcessResource struct {
	ServiceName                string `json:"service.name"`
	ServiceVersion             string `json:"service.version"`
	DeploymentEnvironmentName  string `json:"deployment.environment.name"`
	CellID                     string `json:"accountable.cell.id"`
	RuntimeRole                string `json:"accountable.runtime.role"`
	ReleaseID                  string `json:"accountable.release.id"`
	ConfigurationSchemaVersion int    `json:"accountable.configuration.schema_version"`
	ConfigurationRevision      string `json:"accountable.configuration.revision"`
	ConfigurationFingerprint   string `json:"accountable.configuration.fingerprint"`
	TelemetryProtocol          string `json:"accountable.telemetry.protocol"`
	TelemetrySamplingPolicy    string `json:"accountable.telemetry.sampling.policy"`
	TelemetryRedactionPolicy   string `json:"accountable.telemetry.redaction.policy.version"`
}

func ParseProcessResource(raw []byte) (ProcessResource, error) {
	var resource ProcessResource
	if err := decodeStrictJSON(raw, &resource); err != nil {
		return ProcessResource{}, err
	}
	if err := ValidateProcessResource(resource); err != nil {
		return ProcessResource{}, err
	}
	return resource, nil
}

func ValidateProcessResource(resource ProcessResource) error {
	serviceName, ok := runtimeServices[resource.RuntimeRole]
	if !ok || resource.ServiceName != serviceName {
		return contractError("runtime role service")
	}
	if !digestPattern.MatchString(resource.ServiceVersion) {
		return contractError("service.version")
	}
	if !contains(registeredEnvironments, resource.DeploymentEnvironmentName) {
		return contractError("deployment.environment.name")
	}
	if !cellIDPattern.MatchString(resource.CellID) {
		return contractError("accountable.cell.id")
	}
	if !releaseIDPattern.MatchString(resource.ReleaseID) {
		return contractError("accountable.release.id")
	}
	if err := ValidateConfigIdentity(ConfigIdentity{
		SchemaVersion: resource.ConfigurationSchemaVersion,
		Revision:      resource.ConfigurationRevision,
		Fingerprint:   resource.ConfigurationFingerprint,
	}); err != nil {
		return err
	}
	if resource.TelemetryProtocol != TelemetryProtocol ||
		resource.TelemetrySamplingPolicy != TelemetrySamplingPolicy ||
		resource.TelemetryRedactionPolicy != TelemetryRedactionPolicy {
		return contractError("telemetry policy")
	}
	return nil
}
