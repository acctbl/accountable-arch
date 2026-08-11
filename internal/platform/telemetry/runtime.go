package telemetry

import (
	"net/url"
	"os"
	"strconv"
)

// RuntimeRelease is the verified release identity that one managed process reads.
// It never carries configuration or secret values, so the configuration
// fingerprint stays independent of the release.
type RuntimeRelease struct {
	SchemaVersion         int    `json:"schema_version"`
	ReleaseID             string `json:"release_id"`
	ReleaseManifestDigest string `json:"release_manifest_digest"`
	ServiceVersion        string `json:"service_version"`
	WebAssetDigest        string `json:"web_asset_digest"`
}

// RuntimeConfig is the [telemetry] configuration table for a managed runtime.
type RuntimeConfig struct {
	CollectorEndpoint      string
	Protocol               string
	SamplingPolicy         string
	RedactionPolicyVersion string
}

type RuntimeIdentity struct {
	Release     RuntimeRelease
	Config      ConfigIdentity
	Environment string
	CellID      string
	RuntimeRole string
}

func ParseRuntimeRelease(raw []byte) (RuntimeRelease, error) {
	var release RuntimeRelease
	if err := decodeStrictJSON(raw, &release); err != nil {
		return RuntimeRelease{}, err
	}
	if err := ValidateRuntimeRelease(release); err != nil {
		return RuntimeRelease{}, err
	}
	return release, nil
}

func ReadRuntimeRelease(path string) (RuntimeRelease, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the deployment renders this path into the task-scoped scratch volume
	if err != nil {
		return RuntimeRelease{}, err
	}
	return ParseRuntimeRelease(raw)
}

func ValidateRuntimeRelease(release RuntimeRelease) error {
	if release.SchemaVersion != ContractSchemaVersion {
		return contractError("release schema_version")
	}
	if !uuidV7Pattern.MatchString(release.ReleaseID) {
		return contractError("accountable.release.id")
	}
	if !digestPattern.MatchString(release.ReleaseManifestDigest) {
		return contractError("release_manifest_digest")
	}
	if !digestPattern.MatchString(release.ServiceVersion) {
		return contractError("service.version")
	}
	if !digestPattern.MatchString(release.WebAssetDigest) {
		return contractError("web_asset_digest")
	}
	if release.ServiceVersion == release.ReleaseManifestDigest || release.ServiceVersion == release.WebAssetDigest {
		return contractError("release identity separation")
	}
	return nil
}

// ValidateRuntimeConfig compares the deployed telemetry table with the compiled
// policy versions and fails closed on any difference.
func ValidateRuntimeConfig(config RuntimeConfig) error {
	endpoint, err := url.Parse(config.CollectorEndpoint)
	if err != nil || endpoint.Host == "" || endpoint.User != nil ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "" {
		return contractError("collector_endpoint")
	}
	if endpoint.Scheme != "https" && endpoint.Scheme != "http" {
		return contractError("collector_endpoint")
	}
	if endpoint.Port() != strconv.Itoa(CollectorApplicationPort) {
		return contractError("collector_endpoint")
	}
	if config.Protocol != TelemetryProtocol {
		return contractError("telemetry protocol")
	}
	if config.SamplingPolicy != TelemetrySamplingPolicy {
		return contractError("telemetry sampling_policy")
	}
	if config.RedactionPolicyVersion != TelemetryRedactionPolicy {
		return contractError("telemetry redaction_policy_version")
	}
	return nil
}

// SLIEndpoint derives the delta SLI endpoint from the validated Collector host so
// configuration never carries a second endpoint.
func SLIEndpoint(config RuntimeConfig) (string, error) {
	if err := ValidateRuntimeConfig(config); err != nil {
		return "", err
	}
	endpoint, err := url.Parse(config.CollectorEndpoint)
	if err != nil {
		return "", contractError("collector_endpoint")
	}
	endpoint.Host = endpoint.Hostname() + ":" + strconv.Itoa(CollectorSLIPort)
	return endpoint.String(), nil
}

func BuildProcessResource(identity RuntimeIdentity) (ProcessResource, error) {
	if err := ValidateRuntimeRelease(identity.Release); err != nil {
		return ProcessResource{}, err
	}
	serviceName, ok := runtimeServices[identity.RuntimeRole]
	if !ok {
		return ProcessResource{}, contractError("accountable.runtime.role")
	}
	resource := ProcessResource{
		ServiceName:                serviceName,
		ServiceVersion:             identity.Release.ServiceVersion,
		DeploymentEnvironmentName:  identity.Environment,
		CellID:                     identity.CellID,
		RuntimeRole:                identity.RuntimeRole,
		ReleaseID:                  identity.Release.ReleaseID,
		ConfigurationSchemaVersion: identity.Config.SchemaVersion,
		ConfigurationRevision:      identity.Config.Revision,
		ConfigurationFingerprint:   identity.Config.Fingerprint,
		TelemetryProtocol:          TelemetryProtocol,
		TelemetrySamplingPolicy:    TelemetrySamplingPolicy,
		TelemetryRedactionPolicy:   TelemetryRedactionPolicy,
	}
	if err := ValidateProcessResource(resource); err != nil {
		return ProcessResource{}, err
	}
	return resource, nil
}
