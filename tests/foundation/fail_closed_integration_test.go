package foundation_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/acctbl/accountable/internal/migration"
	platformdb "github.com/acctbl/accountable/internal/platform/database"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestFoundationFailClosedPostgresIntegration(t *testing.T) {
	dsn := os.Getenv("ACCOUNTABLE_TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("ACCOUNTABLE_REQUIRE_POSTGRES") == "1" {
			t.Fatal("ACCOUNTABLE_TEST_POSTGRES_DSN is required")
		}
		t.Skip("run through task test:postgres")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	binDir := t.TempDir()
	binaries := map[string]string{
		"preflight": filepath.Join(binDir, "preflight"),
		"api":       filepath.Join(binDir, "api"),
	}
	for name, binary := range binaries {
		command := exec.Command("go", "build", "-o", binary, "./cmd/"+name)
		command.Dir = root
		if output, buildErr := command.CombinedOutput(); buildErr != nil {
			t.Fatalf("build %s: %v\n%s", name, buildErr, output)
		}
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	defer func() { _ = database.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	parsedDSN, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	port, err := strconv.ParseUint(parsedDSN.Port(), 10, 16)
	if err != nil {
		t.Fatalf("parse DSN port: %v", err)
	}
	password, _ := parsedDSN.User.Password()
	if password == "" {
		password = os.Getenv("ACCOUNTABLE_TEST_POSTGRES_PASSWORD")
	}

	type faultCase struct {
		name       string
		mutateTOML func(string) string
		setup      func(*testing.T, string, string)
		binaries   []string
	}
	allBinaries := []string{"preflight", "api"}
	cases := []faultCase{
		{name: "unknown TOML key", mutateTOML: func(value string) string { return value + "\nsurprise = true\n" }, binaries: allBinaries},
		{name: "capability contradiction", mutateTOML: func(value string) string { return strings.Replace(value, "kms = true", "kms = false", 1) }, binaries: allBinaries},
		{name: "out of range timeout", mutateTOML: func(value string) string {
			return strings.Replace(value, `foundation_check_timeout = "30s"`, `foundation_check_timeout = "0s"`, 1)
		}, binaries: allBinaries},
		{name: "unresolvable secret", mutateTOML: func(value string) string {
			return strings.Replace(value, `password_ref = "database.password"`, `password_ref = "database.missing"`, 1)
		}, binaries: allBinaries},
		{name: "empty secret", setup: func(t *testing.T, secrets, _ string) { writeFile(t, filepath.Join(secrets, "database.password"), nil) }, binaries: allBinaries},
		{name: "wrong crypto key", setup: func(t *testing.T, secrets, _ string) {
			writeFile(t, filepath.Join(secrets, "crypto.primary_key"), []byte("not-a-key"))
		}, binaries: allBinaries},
		{name: "unwritable storage root", setup: func(t *testing.T, _, storage string) {
			if err := os.Chmod(storage, 0o500); err != nil {
				t.Fatalf("make storage unwritable: %v", err)
			}
			t.Cleanup(func() { _ = os.Chmod(storage, 0o700) })
		}, binaries: allBinaries},
		{name: "non UTC session contract", mutateTOML: func(value string) string {
			return strings.Replace(value, `timezone = "UTC"`, `timezone = "Europe/London"`, 1)
		}, binaries: allBinaries},
		{name: "schema behind", setup: func(t *testing.T, _, _ string) {
			if _, err := database.ExecContext(ctx, `DROP TABLE goose_db_version, accountable_schema_state`); err != nil {
				t.Fatalf("seed behind schema: %v", err)
			}
		}, binaries: allBinaries},
		{name: "schema ahead", setup: schemaStateSetup(database, ctx, platformdb.MaximumSchemaVersion+1, false), binaries: allBinaries},
		{name: "schema dirty", setup: schemaStateSetup(database, ctx, platformdb.MaximumSchemaVersion, true), binaries: allBinaries},
		{name: "AWS region and ARN mismatch", mutateTOML: func(string) string { return managedRegionMismatchConfig() }, binaries: allBinaries},
		{name: "runtime role and binary mismatch", mutateTOML: func(value string) string {
			return strings.Replace(value, `runtime_role = "api"`, `runtime_role = "migrate"`, 1)
		}, binaries: []string{"api"}},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := database.ExecContext(ctx, `DROP TABLE IF EXISTS accountable_proof_nonces, goose_db_version, accountable_schema_state`); err != nil {
				t.Fatalf("reset database: %v", err)
			}
			if err := migration.Run(ctx, database); err != nil {
				t.Fatalf("migrate baseline: %v", err)
			}
			caseRoot := t.TempDir()
			secrets := filepath.Join(caseRoot, "secrets")
			storage := filepath.Join(caseRoot, "storage")
			if err := os.Mkdir(secrets, 0o700); err != nil {
				t.Fatalf("create secrets: %v", err)
			}
			if err := os.Mkdir(storage, 0o700); err != nil {
				t.Fatalf("create storage: %v", err)
			}
			writeFile(t, filepath.Join(secrets, "database.password"), []byte(password))
			writeFile(t, filepath.Join(secrets, "crypto.primary_key"), []byte("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="))
			if test.setup != nil {
				test.setup(t, secrets, storage)
			}
			address := reserveAddress(t)
			config := developmentAPIConfig(parsedDSN.Hostname(), uint16(port), parsedDSN.Path[1:], parsedDSN.User.Username(), secrets, storage, address)
			if test.mutateTOML != nil {
				config = test.mutateTOML(config)
			}
			configPath := filepath.Join(caseRoot, "api.toml")
			writeFile(t, configPath, []byte(config))
			before := schemaSnapshot(t, ctx, database)
			for _, binaryName := range test.binaries {
				commandCtx, stop := context.WithTimeout(ctx, 10*time.Second)
				command := exec.CommandContext(commandCtx, binaries[binaryName], "--config", configPath)
				output, runErr := command.CombinedOutput()
				contextErr := commandCtx.Err()
				stop()
				if contextErr != nil {
					t.Fatalf("%s did not fail before serving: %v", binaryName, contextErr)
				}
				if runErr == nil {
					t.Fatalf("%s accepted injected fault; output: %s", binaryName, output)
				}
				var exitError *exec.ExitError
				if !errors.As(runErr, &exitError) {
					t.Fatalf("%s execution error = %v", binaryName, runErr)
				}
				if strings.Contains(string(output), password) || strings.Contains(string(output), "AAAAAAAAAAAAAAAA") {
					t.Fatalf("%s leaked secret material: %s", binaryName, output)
				}
				if after := schemaSnapshot(t, ctx, database); after != before {
					t.Fatalf("%s mutated database: before %q after %q", binaryName, before, after)
				}
				listener, listenErr := net.Listen("tcp", address)
				if listenErr != nil {
					t.Fatalf("%s left listener active at %s: %v", binaryName, address, listenErr)
				}
				_ = listener.Close()
			}
		})
	}
}

func schemaStateSetup(database *sql.DB, ctx context.Context, version int64, dirty bool) func(*testing.T, string, string) {
	return func(t *testing.T, _, _ string) {
		t.Helper()
		if _, err := database.ExecContext(ctx, `UPDATE accountable_schema_state SET version = $1, dirty = $2`, version, dirty); err != nil {
			t.Fatalf("seed schema state: %v", err)
		}
	}
}

func schemaSnapshot(t *testing.T, ctx context.Context, database *sql.DB) string {
	t.Helper()
	var exists bool
	if err := database.QueryRowContext(ctx, `SELECT to_regclass('public.accountable_schema_state') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatalf("check schema state table: %v", err)
	}
	if !exists {
		return "absent"
	}
	var version int64
	var dirty bool
	if err := database.QueryRowContext(ctx, `SELECT version, dirty FROM accountable_schema_state WHERE singleton = TRUE`).Scan(&version, &dirty); err != nil {
		t.Fatalf("read schema state: %v", err)
	}
	return fmt.Sprintf("%d:%t", version, dirty)
}

func reserveAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve API address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release API address: %v", err)
	}
	return address
}

func writeFile(t *testing.T, path string, value []byte) {
	t.Helper()
	if err := os.WriteFile(path, value, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func developmentAPIConfig(host string, port uint16, name, user, secrets, storage, address string) string {
	return fmt.Sprintf(`schema_version = 1
revision = "integration"
environment = "development"
deployment_mode = "local"
cell_id = "local"
aws_region = "eu-west-2"
runtime_role = "api"
foundation_check_timeout = "30s"
readiness_probe_interval = "100ms"

[capabilities]
architecture_probe = false
postgres = true
secrets = true
kms = true
object_storage = true
telemetry = false
redpanda = false

[server]
listen_address = %q
allowed_origins = []
trusted_proxy_cidrs = []
unary_rpc_timeout = "10s"
stream_rpc_timeout = "25s"

[secrets]
provider = "file"
directory = %q

[postgres]
host = %q
port = %d
name = %q
user = %q
role = %q
password_ref = "database.password"
tls_mode = "disable"
connect_timeout = "2s"
statement_timeout = "5s"
health_check_interval = "100ms"
max_connections = 4
timezone = "UTC"

[object_storage]
provider = "filesystem"
root = %q
access_purpose = "foundation-proof"

[kms]
provider = "local"
key_ref = "crypto.primary_key"
encryption_context_prefix = "accountable.foundation"

[time]
provider = "system"
max_clock_error = "1s"
max_database_skew = "5s"
`, address, secrets, host, port, name, user, user, storage)
}

func managedRegionMismatchConfig() string {
	return `schema_version = 1
revision = "integration"
environment = "production"
deployment_mode = "managed"
cell_id = "cell-a"
aws_region = "eu-west-2"
runtime_role = "api"
foundation_check_timeout = "30s"
readiness_probe_interval = "10s"

[capabilities]
architecture_probe = false
postgres = true
secrets = true
kms = true
object_storage = true
telemetry = false
redpanda = false

[server]
listen_address = "127.0.0.1:18080"
allowed_origins = ["https://shell.example"]
trusted_proxy_cidrs = []
unary_rpc_timeout = "10s"
stream_rpc_timeout = "25s"

[secrets]
provider = "aws_parameter_store"
aws_region = "eu-west-2"
parameter_path = "/accountable/cell-a/production/api"

[postgres]
host = "postgres.internal"
port = 5432
name = "accountable"
user = "accountable_login"
role = "accountable_api"
password_ref = "database-password"
tls_mode = "verify-full"
connect_timeout = "5s"
statement_timeout = "10s"
health_check_interval = "10s"
max_connections = 16
timezone = "UTC"

[object_storage]
provider = "s3"
region = "us-east-1"
bucket = "accountable-private"
prefix = "application"
expected_owner = "123456789012"
kms_key_arn = "arn:aws:kms:us-east-1:123456789012:key/11111111-1111-1111-1111-111111111111"
access_purpose = "foundation-proof"

[kms]
provider = "aws_kms"
region = "eu-west-2"
kms_key_arn = "arn:aws:kms:eu-west-2:123456789012:key/22222222-2222-2222-2222-222222222222"
encryption_context_prefix = "accountable.foundation"

[time]
provider = "linux"
max_clock_error = "1s"
max_database_skew = "1s"
`
}
