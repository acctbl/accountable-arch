package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAPIConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "api.toml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func completeAPIConfig(environment string, architectureProbe bool) string {
	managed := environment == "staging" || environment == "production"
	deploymentMode := "local"
	secrets := `[secrets]
provider = "file"
directory = "secrets"`
	postgres := `[postgres]
host = "127.0.0.1"
port = 5432
name = "accountable"
user = "postgres"
role = "postgres"
password_ref = "database.password"
tls_mode = "disable"
connect_timeout = "5s"
statement_timeout = "10s"
health_check_interval = "2s"
max_connections = 16
timezone = "UTC"`
	objectStorage := `[object_storage]
provider = "filesystem"
root = "storage"
access_purpose = "foundation-proof"`
	kms := `[kms]
provider = "local"
key_ref = "crypto.primary_key"
encryption_context_prefix = "accountable.foundation"`
	timeConfig := `[time]
provider = "system"
max_clock_error = "1s"
max_database_skew = "1s"`
	if managed {
		deploymentMode = "managed"
		secrets = fmt.Sprintf(`[secrets]
provider = "aws_parameter_store"
aws_region = "eu-west-2"
parameter_path = "/accountable/cell-a/%s/api"`, environment)
		postgres = `[postgres]
host = "postgres.internal"
port = 5432
name = "accountable"
user = "accountable_login"
role = "accountable_api"
password_ref = "database-password"
tls_mode = "verify-full"
connect_timeout = "5s"
statement_timeout = "10s"
health_check_interval = "2s"
max_connections = 16
timezone = "UTC"`
		objectStorage = `[object_storage]
provider = "s3"
region = "eu-west-2"
bucket = "accountable-private"
prefix = "application/"
expected_owner = "123456789012"
kms_key_arn = "arn:aws:kms:eu-west-2:123456789012:key/11111111-1111-1111-1111-111111111111"
access_purpose = "foundation-proof"`
		kms = `[kms]
provider = "aws_kms"
region = "eu-west-2"
kms_key_arn = "arn:aws:kms:eu-west-2:123456789012:key/22222222-2222-2222-2222-222222222222"
encryption_context_prefix = "accountable.foundation"`
		timeConfig = `[time]
provider = "linux"
max_clock_error = "1s"
max_database_skew = "1s"`
	}
	return fmt.Sprintf(`schema_version = 1
revision = "reviewed-1"
environment = %q
deployment_mode = %q
cell_id = "cell-a"
aws_region = "eu-west-2"
runtime_role = "api"
foundation_check_timeout = "30s"
readiness_probe_interval = "10s"

[capabilities]
architecture_probe = %t
postgres = true
secrets = true
kms = true
object_storage = true
telemetry = true
redpanda = false

[telemetry]
collector_endpoint = "http://collector.development-01.accountable.internal:4318"
protocol = "otlp_http"
sampling_policy = "parent_based_5pct_v1"
redaction_policy_version = "allowlist_v1"

[client_telemetry]
release_id = "018f47d2-7f00-7abc-8def-1234567890ab"
web_asset_digest = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
configuration_schema_version = 1
configuration_revision = "reviewed-1"
configuration_fingerprint = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
proof_key_active_ref = "proof-key-active"
proof_key_standby_ref = "proof-key-standby"

[server]
listen_address = "0.0.0.0:8080"
allowed_origins = ["https://shell.example"]
trusted_proxy_cidrs = ["10.0.0.0/8"]
unary_rpc_timeout = "10s"
stream_rpc_timeout = "25s"

%s

%s

%s

%s

%s
`, environment, deploymentMode, architectureProbe, secrets, postgres, objectStorage, kms, timeConfig)
}

func TestConfigRequiresExplicitCapabilitiesAndSections(t *testing.T) {
	t.Parallel()

	path := writeAPIConfig(t, `schema_version = 1
revision = "reviewed-1"
environment = "development"
deployment_mode = "local"
cell_id = "local"
aws_region = "eu-west-2"
runtime_role = "api"`)
	_, err := loadConfig([]string{"--config", path})
	if err == nil || !strings.Contains(err.Error(), "capability") {
		t.Fatalf("loadConfig error = %v, want incomplete capability refusal", err)
	}
}

func TestRunRequiresOneAbsoluteConfigPath(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{nil, {"--config", "api.toml"}, {"--config", "/tmp/api.toml", "unexpected"}} {
		err := run(context.Background(), args)
		if err == nil || !strings.Contains(err.Error(), "--config") {
			t.Fatalf("run(%q) error = %v, want --config usage error", args, err)
		}
	}
}

func TestManagedConfigIsCompleteAndExplicit(t *testing.T) {
	t.Parallel()

	path := writeAPIConfig(t, completeAPIConfig("staging", false))
	loaded, err := loadConfig([]string{"--config", path})
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if loaded.Environment != "staging" || loaded.Addr != "0.0.0.0:8080" ||
		loaded.Foundation.Secrets.Provider != "aws_parameter_store" || loaded.Foundation.Storage.Provider != "s3" ||
		len(loaded.Foundation.Fingerprint) != 64 {
		t.Fatalf("loaded config = %+v", loaded)
	}
}

func TestProductionConfigRejectsArchitectureProbe(t *testing.T) {
	t.Parallel()

	path := writeAPIConfig(t, completeAPIConfig("production", true))
	if _, err := loadConfig([]string{"--config", path}); err == nil || !strings.Contains(err.Error(), "architecture_probe") {
		t.Fatalf("loadConfig error = %v, want production probe rejection", err)
	}
}

func TestConfigRejectsWrongRuntimeRole(t *testing.T) {
	t.Parallel()

	contents := strings.Replace(completeAPIConfig("development", false), `runtime_role = "api"`, `runtime_role = "migrate"`, 1)
	path := writeAPIConfig(t, contents)
	if _, err := loadConfig([]string{"--config", path}); err == nil || !strings.Contains(err.Error(), "runtime_role") {
		t.Fatalf("loadConfig error = %v, want role refusal", err)
	}
}

func TestConfigFingerprintUsesExactFileBytes(t *testing.T) {
	t.Parallel()

	contents := completeAPIConfig("development", false)
	path := writeAPIConfig(t, contents)
	first, err := loadConfig([]string{"--config", path})
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	second, err := loadConfig([]string{"--config", path})
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if first.Foundation.Fingerprint != second.Foundation.Fingerprint {
		t.Fatal("unchanged bytes produced different fingerprints")
	}
	if err := os.WriteFile(path, []byte(contents+"\n"), 0o600); err != nil {
		t.Fatalf("change one byte: %v", err)
	}
	changed, err := loadConfig([]string{"--config", path})
	if err != nil {
		t.Fatalf("changed load: %v", err)
	}
	if changed.Foundation.Fingerprint == first.Foundation.Fingerprint {
		t.Fatal("changed bytes produced the same fingerprint")
	}
}

func TestConfigRejectsUnsafeServerAndUnknownFields(t *testing.T) {
	t.Parallel()

	for name, contents := range map[string]string{
		"non-origin CORS": strings.Replace(completeAPIConfig("development", false), `https://shell.example`, `https://shell.example/path`, 1),
		"relative TLS":    strings.Replace(completeAPIConfig("development", false), `stream_rpc_timeout = "25s"`, "stream_rpc_timeout = \"25s\"\ntls_certificate_file = \"cert.pem\"\ntls_private_key_file = \"key.pem\"", 1),
		"unknown field":   strings.Replace(completeAPIConfig("development", false), `revision = "reviewed-1"`, "revision = \"reviewed-1\"\nsurprise = \"not allowed\"", 1),
		"inline secret":   strings.Replace(completeAPIConfig("development", false), `password_ref = "database.password"`, "password_ref = \"database.password\"\npassword = \"not-allowed-inline\"", 1),
	} {
		path := writeAPIConfig(t, contents)
		if _, err := loadConfig([]string{"--config", path}); err == nil {
			t.Errorf("%s: loadConfig accepted unsafe input", name)
		}
	}
}
