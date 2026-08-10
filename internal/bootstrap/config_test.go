package bootstrap

import (
	"testing"

	"github.com/acctbl/accountable/internal/platform/crypto"
	"github.com/acctbl/accountable/internal/platform/database"
	"github.com/acctbl/accountable/internal/platform/secret"
	"github.com/acctbl/accountable/internal/platform/storage"
)

func TestParseRejectsNonUTCDatabaseTimezone(t *testing.T) {
	t.Parallel()

	raw := developmentFileConfig()
	raw.Postgres.Timezone = "Europe/London"
	if _, err := Parse("/tmp/api.toml", "fingerprint", raw); err == nil {
		t.Fatal("Parse accepted non-UTC database timezone")
	}
}

func TestParseAcceptsExplicitManagedFoundation(t *testing.T) {
	t.Parallel()

	raw := managedFileConfig()
	config, err := Parse("/etc/accountable/api.toml", "fingerprint", raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if config.Database.PasswordRef != "database-password" || config.Database.TLSMode != database.TLSVerifyFull ||
		config.Secrets.Provider != secret.ProviderParameterStore || config.Storage.Provider != storage.ProviderS3 ||
		config.Crypto.Provider != crypto.ProviderAWSKMS || config.Time.Provider != TimeProviderLinux ||
		config.DeploymentMode != DeploymentModeManaged || config.Fingerprint != "fingerprint" ||
		config.Revision != "reviewed-1" {
		t.Fatalf("managed config = %+v", config)
	}
}

func TestParseAcceptsManagedDevelopmentFoundation(t *testing.T) {
	t.Parallel()

	raw := managedFileConfig()
	raw.Environment = "development"
	config, err := Parse("/etc/accountable/api.toml", "fingerprint", raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if config.Environment != "development" || config.DeploymentMode != DeploymentModeManaged {
		t.Fatalf("managed development config = %+v", config)
	}
}

func TestParseRefusesLocalProvidersInStaging(t *testing.T) {
	t.Parallel()

	raw := developmentFileConfig()
	raw.Environment = "staging"
	_, err := Parse("/tmp/api.toml", "fingerprint", raw)
	if err == nil {
		t.Fatalf("Parse error = %v, want managed-provider refusal", err)
	}
}

func TestParseRefusesLocalDeploymentOutsideDevelopment(t *testing.T) {
	t.Parallel()

	for _, environment := range []string{"staging", "production"} {
		raw := developmentFileConfig()
		raw.Environment = environment
		if _, err := Parse("/tmp/api.toml", "fingerprint", raw); err == nil {
			t.Errorf("Parse accepted local %s deployment", environment)
		}
	}
}

func TestParseRefusesUnsafeParameterPaths(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]string{
		"relative":   "accountable/cell-a/production/api",
		"traversing": "/accountable/cell-a/../production/api",
		"trailing":   "/accountable/cell-a/production/api/",
		"root":       "/",
		"empty":      "",
	} {
		raw := managedFileConfig()
		raw.Secrets.ParameterPath = value
		if _, err := Parse("/etc/accountable/api.toml", "fingerprint", raw); err == nil {
			t.Errorf("%s: Parse accepted parameter_path %q", name, value)
		}
	}
}

func TestParseRefusesCapabilityContradictions(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(*FileConfig){
		"missing explicit value":          func(raw *FileConfig) { raw.Capabilities.KMS = nil },
		"section present while false":     func(raw *FileConfig) { raw.Capabilities.KMS = boolPointer(false) },
		"section absent while true":       func(raw *FileConfig) { raw.KMS = nil },
		"telemetry before implementation": func(raw *FileConfig) { raw.Capabilities.Telemetry = boolPointer(true) },
		"redpanda before implementation":  func(raw *FileConfig) { raw.Capabilities.Redpanda = boolPointer(true) },
		"production architecture probe":   func(raw *FileConfig) { raw.Capabilities.ArchitectureProbe = boolPointer(true) },
	} {
		raw := managedFileConfig()
		mutate(&raw)
		if _, err := Parse("/etc/accountable/api.toml", "fingerprint", raw); err == nil {
			t.Errorf("%s: Parse accepted contradiction", name)
		}
	}
}

func TestParseRefusesAWSRegionMismatch(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(*FileConfig){
		"Parameter Store": func(raw *FileConfig) { raw.Secrets.AWSRegion = "us-east-1" },
		"object storage":  func(raw *FileConfig) { raw.ObjectStorage.Region = "us-east-1" },
		"KMS":             func(raw *FileConfig) { raw.KMS.Region = "us-east-1" },
	} {
		raw := managedFileConfig()
		mutate(&raw)
		if _, err := Parse("/etc/accountable/api.toml", "fingerprint", raw); err == nil {
			t.Errorf("%s: Parse accepted region mismatch", name)
		}
	}
}

func managedFileConfig() FileConfig {
	return FileConfig{
		SchemaVersion: 1, Revision: "reviewed-1", Environment: "production",
		DeploymentMode: DeploymentModeManaged, CellID: "cell-a",
		AWSRegion: "eu-west-2", RuntimeRole: RuntimeRoleAPI, CheckTimeout: "30s", ReadinessProbeInterval: "10s",
		Capabilities: completeCapabilities(false),
		Secrets: &SecretsFileConfig{
			Provider: secret.ProviderParameterStore, AWSRegion: "eu-west-2",
			ParameterPath: "/accountable/cell-a/production/api",
		},
		Postgres: &DatabaseFileConfig{
			Host: "postgres.internal", Port: 5432, Name: "accountable", User: "accountable_login", Role: "accountable_api",
			PasswordRef: "database-password", TLSMode: database.TLSVerifyFull, ConnectTimeout: "5s",
			StatementTimeout: "10s", HealthCheckInterval: "30s", MaxConnections: 16, Timezone: "UTC",
		},
		ObjectStorage: &StorageFileConfig{
			Provider: storage.ProviderS3, Region: "eu-west-2", Bucket: "accountable-production-private",
			Prefix: "application/", ExpectedOwner: "123456789012", AccessPurpose: AccessPurposeFoundationProof,
			KMSKeyARN: "arn:aws:kms:eu-west-2:123456789012:key/11111111-1111-1111-1111-111111111111",
		},
		KMS: &CryptoFileConfig{
			Provider: crypto.ProviderAWSKMS, Region: "eu-west-2",
			KMSKeyARN:               "arn:aws:kms:eu-west-2:123456789012:key/22222222-2222-2222-2222-222222222222",
			EncryptionContextPrefix: "accountable.foundation",
		},
		Time: &TimeFileConfig{Provider: TimeProviderLinux, MaxClockError: "1s", MaxDatabaseSkew: "1s"},
	}
}

func developmentFileConfig() FileConfig {
	return FileConfig{
		SchemaVersion: 1, Revision: "reviewed-1", Environment: "development",
		DeploymentMode: DeploymentModeLocal, CellID: "local",
		AWSRegion: "eu-west-2", RuntimeRole: RuntimeRoleAPI, CheckTimeout: "10s", ReadinessProbeInterval: "1s",
		Capabilities: completeCapabilities(true),
		Secrets:      &SecretsFileConfig{Provider: secret.ProviderFile, Directory: "secrets"},
		Postgres: &DatabaseFileConfig{
			Host: "127.0.0.1", Port: 5432, Name: "accountable", User: "postgres", Role: "postgres",
			PasswordRef: "database.password", TLSMode: database.TLSDisable, ConnectTimeout: "1s",
			StatementTimeout: "1s", HealthCheckInterval: "1s", MaxConnections: 1, Timezone: "UTC",
		},
		ObjectStorage: &StorageFileConfig{
			Provider: storage.ProviderFile, Root: "storage", AccessPurpose: AccessPurposeFoundationProof,
		},
		KMS: &CryptoFileConfig{
			Provider: crypto.ProviderLocal, KeyRef: "crypto.key", EncryptionContextPrefix: "accountable.foundation",
		},
		Time: &TimeFileConfig{Provider: TimeProviderSystem, MaxClockError: "1s", MaxDatabaseSkew: "1s"},
	}
}

func completeCapabilities(architectureProbe bool) CapabilitiesFileConfig {
	return CapabilitiesFileConfig{
		ArchitectureProbe: boolPointer(architectureProbe), Postgres: boolPointer(true), Secrets: boolPointer(true),
		KMS: boolPointer(true), ObjectStorage: boolPointer(true), Telemetry: boolPointer(false), Redpanda: boolPointer(false),
	}
}

func boolPointer(value bool) *bool { return &value }
