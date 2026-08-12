package migration_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"testing/fstest"
	"time"

	"github.com/acctbl/accountable/internal/bootstrap"
	"github.com/acctbl/accountable/internal/migration"
	"github.com/acctbl/accountable/internal/platform/clock"
	"github.com/acctbl/accountable/internal/platform/crypto"
	platformdb "github.com/acctbl/accountable/internal/platform/database"
	"github.com/acctbl/accountable/internal/platform/secret"
	"github.com/acctbl/accountable/internal/platform/storage"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type staticResolver struct{ password string }

func (r staticResolver) ResolveBatch(_ context.Context, refs []secret.Ref) (map[secret.Ref]secret.SecretValue, error) {
	values := make(map[secret.Ref]secret.SecretValue, len(refs))
	for _, ref := range refs {
		values[ref] = secret.NewSecretValue([]byte(r.password))
	}
	return values, nil
}

func TestPostgresIntegrationRefusalMatrix(t *testing.T) {
	dsn := os.Getenv("ACCOUNTABLE_TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("ACCOUNTABLE_REQUIRE_POSTGRES") == "1" {
			t.Fatal("ACCOUNTABLE_TEST_POSTGRES_DSN is required")
		}
		t.Skip("run through task test:postgres")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	defer func() { _ = database.Close() }()
	reset := func() {
		t.Helper()
		if _, err := database.ExecContext(ctx, `DROP TABLE IF EXISTS accountable_proof_nonces, goose_db_version, accountable_schema_state`); err != nil {
			t.Fatalf("reset database: %v", err)
		}
	}
	reset()
	defer reset()

	databaseConfig, password := databaseConfigFromDSN(t, dsn)
	opened, err := platformdb.OpenDatabase(ctx, databaseConfig, staticResolver{password: password}, nil)
	if !errors.Is(err, platformdb.ErrSchemaBehind) || opened != nil {
		t.Fatalf("empty database open = (%v, %v), want schema behind", opened, err)
	}
	var stateTableExists bool
	if err := database.QueryRowContext(ctx, `SELECT to_regclass('public.accountable_schema_state') IS NOT NULL`).Scan(&stateTableExists); err != nil {
		t.Fatalf("check state table: %v", err)
	}
	if stateTableExists {
		t.Fatal("API database preflight mutated an empty schema")
	}

	if _, err := database.ExecContext(ctx, `SET TIME ZONE 'Europe/London'`); err != nil {
		t.Fatalf("seed non-UTC migration session: %v", err)
	}
	if err := migration.Run(ctx, database, migration.Catalogue()...); err != nil {
		t.Fatalf("migrate empty database: %v", err)
	}
	var migrationTimezone string
	if err := database.QueryRowContext(ctx, `SELECT current_setting('TimeZone')`).Scan(&migrationTimezone); err != nil {
		t.Fatalf("read migration timezone: %v", err)
	}
	if migrationTimezone != "UTC" {
		t.Fatalf("migration timezone = %q, want UTC", migrationTimezone)
	}
	opened, err = platformdb.OpenDatabase(ctx, databaseConfig, staticResolver{password: password}, nil)
	if err != nil {
		t.Fatalf("open compatible database: %v", err)
	}
	if err := opened.CheckClock(ctx, clock.System{}, 5*time.Second); err != nil {
		t.Fatalf("check database clock: %v", err)
	}
	if err := opened.CheckClock(ctx, clock.Fixed{Instant: time.Unix(0, 0)}, time.Second); !errors.Is(err, platformdb.ErrDatabaseClockSkew) {
		t.Fatalf("bad database clock check = %v, want skew refusal", err)
	}
	opened.Close()
	root := t.TempDir()
	secretDirectory := filepath.Join(root, "secrets")
	storageDirectory := filepath.Join(root, "storage")
	if err := os.Mkdir(secretDirectory, 0o700); err != nil {
		t.Fatalf("create secret directory: %v", err)
	}
	if err := os.Mkdir(storageDirectory, 0o700); err != nil {
		t.Fatalf("create storage directory: %v", err)
	}
	writeSecret := func(name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(secretDirectory, name), []byte(value), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	writeSecret(string(databaseConfig.PasswordRef), password)
	writeSecret("crypto.key", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	dependencies, err := bootstrap.Build(ctx, bootstrap.Config{
		Environment:  "development",
		CheckTimeout: 30 * time.Second,
		Capabilities: bootstrap.Capabilities{
			Postgres: true, Secrets: true, KMS: true, ObjectStorage: true,
		},
		Secrets:  secret.Config{Provider: secret.ProviderFile, Directory: secretDirectory},
		Database: databaseConfig,
		Storage:  storage.Config{Provider: storage.ProviderFile, Root: storageDirectory},
		Crypto:   crypto.Config{Provider: crypto.ProviderLocal, KeyRef: secret.Ref("crypto.key")},
		Time: bootstrap.TimeConfig{
			Provider: bootstrap.TimeProviderSystem, MaxClockError: time.Second, MaxDatabaseSkew: 5 * time.Second,
		},
	}, nil)
	if err != nil {
		t.Fatalf("build complete local foundation: %v", err)
	}
	dependencies.Close()
	if _, err := database.ExecContext(ctx, `
CREATE TABLE migration_state_update_audit (updated_at timestamptz NOT NULL DEFAULT now());
CREATE FUNCTION audit_migration_state_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO migration_state_update_audit DEFAULT VALUES;
    RETURN NEW;
END
$$;
CREATE TRIGGER audit_migration_state_update
AFTER UPDATE ON public.accountable_schema_state
FOR EACH ROW EXECUTE FUNCTION audit_migration_state_update()`); err != nil {
		t.Fatalf("install schema-state audit: %v", err)
	}
	if err := migration.Run(ctx, database, migration.Catalogue()...); err != nil {
		t.Fatalf("run no-op migration: %v", err)
	}
	var stateUpdates int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM migration_state_update_audit`).Scan(&stateUpdates); err != nil {
		t.Fatalf("count schema-state updates: %v", err)
	}
	if stateUpdates != 0 {
		t.Fatalf("no-op migration changed schema state %d times", stateUpdates)
	}
	if _, err := database.ExecContext(ctx, `
DROP TRIGGER audit_migration_state_update ON public.accountable_schema_state;
DROP FUNCTION audit_migration_state_update();
DROP TABLE migration_state_update_audit`); err != nil {
		t.Fatalf("remove schema-state audit: %v", err)
	}
	if _, err := database.ExecContext(ctx, `DELETE FROM public.accountable_schema_state`); err != nil {
		t.Fatalf("delete schema state: %v", err)
	}
	opened, err = platformdb.OpenDatabase(ctx, databaseConfig, staticResolver{password: password}, nil)
	if opened != nil {
		opened.Close()
	}
	if !errors.Is(err, platformdb.ErrSchemaBehind) {
		t.Fatalf("missing schema row error = %v, want schema behind", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO public.accountable_schema_state (singleton, version, dirty) VALUES (TRUE, $1, FALSE)`, platformdb.MaximumSchemaVersion); err != nil {
		t.Fatalf("restore schema state: %v", err)
	}

	assertRefused := func(name string, version int64, dirty bool, want error) {
		t.Helper()
		if _, err := database.ExecContext(ctx, `UPDATE accountable_schema_state SET version = $1, dirty = $2`, version, dirty); err != nil {
			t.Fatalf("%s seed state: %v", name, err)
		}
		opened, err := platformdb.OpenDatabase(ctx, databaseConfig, staticResolver{password: password}, nil)
		if opened != nil {
			opened.Close()
		}
		if !errors.Is(err, want) {
			t.Fatalf("%s open error = %v, want %v", name, err, want)
		}
	}
	assertRefused("behind", platformdb.MinimumSchemaVersion-1, false, platformdb.ErrSchemaBehind)
	assertRefused("dirty", platformdb.MinimumSchemaVersion, true, platformdb.ErrSchemaDirty)
	assertRefused("unknown ahead", platformdb.MaximumSchemaVersion+1, false, platformdb.ErrSchemaAhead)

	reset()
	if err := migration.Run(ctx, database, migration.Catalogue()...); err != nil {
		t.Fatalf("seed schema before failed migration: %v", err)
	}
	badMigrations := fstest.MapFS{
		"20260812000000_bad_invalid.sql": &fstest.MapFile{Data: []byte("-- +goose Up\nTHIS IS NOT SQL;\n")},
	}
	if err := migration.Run(ctx, database, migration.Source{Owner: "bad", FS: fs.FS(badMigrations)}); err == nil {
		t.Fatal("invalid migration succeeded")
	}
	var dirty bool
	if err := database.QueryRowContext(ctx, `SELECT dirty FROM accountable_schema_state WHERE singleton = TRUE`).Scan(&dirty); err != nil {
		t.Fatalf("read dirty state: %v", err)
	}
	if !dirty {
		t.Fatal("failed migration did not leave schema dirty")
	}
}

func TestPostgresIntegrationConcurrentMigrators(t *testing.T) {
	dsn := os.Getenv("ACCOUNTABLE_TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("ACCOUNTABLE_REQUIRE_POSTGRES") == "1" {
			t.Fatal("ACCOUNTABLE_TEST_POSTGRES_DSN is required")
		}
		t.Skip("run through task test:postgres")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	first, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open first Postgres connection: %v", err)
	}
	defer func() { _ = first.Close() }()
	second, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open second Postgres connection: %v", err)
	}
	defer func() { _ = second.Close() }()
	if _, err := first.ExecContext(ctx, `DROP TABLE IF EXISTS accountable_proof_nonces, goose_db_version, accountable_schema_state`); err != nil {
		t.Fatalf("reset database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = first.ExecContext(context.Background(), `DROP TABLE IF EXISTS accountable_proof_nonces, goose_db_version, accountable_schema_state`)
	})

	slowMigration := fstest.MapFS{
		"20260801000000_platform_init.sql": &fstest.MapFile{Data: []byte(`-- +goose Up
-- +goose StatementBegin
CREATE TABLE public.accountable_schema_state (
    singleton boolean PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    version bigint NOT NULL,
    dirty boolean NOT NULL
);
INSERT INTO public.accountable_schema_state (singleton, version, dirty) VALUES (TRUE, 0, FALSE);
SELECT pg_sleep(0.3);
-- +goose StatementEnd
`)},
	}
	source := migration.Source{Owner: "platform/database", FS: fs.FS(slowMigration)}
	firstDone := make(chan error, 1)
	go func() { firstDone <- migration.Run(ctx, first, source) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		var locked bool
		if err := second.QueryRowContext(ctx, `SELECT EXISTS (
            SELECT 1 FROM pg_locks WHERE locktype = 'advisory' AND granted
        )`).Scan(&locked); err != nil {
			t.Fatalf("observe migration lock: %v", err)
		}
		if locked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first migrator did not acquire its advisory lock")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := migration.Run(ctx, second, source); err == nil {
		t.Fatal("second migrator bypassed the advisory lock")
	}
	if err := <-firstDone; err != nil {
		t.Fatalf("first migrator: %v", err)
	}
	var version int64
	var dirty bool
	if err := second.QueryRowContext(ctx, `SELECT version, dirty FROM accountable_schema_state WHERE singleton = TRUE`).Scan(&version, &dirty); err != nil {
		t.Fatalf("read final schema state: %v", err)
	}
	const slowMigrationVersion int64 = 20_260_801_000_000
	if version != slowMigrationVersion || dirty {
		t.Fatalf("final schema state = (%d, %t), want (%d, false)", version, dirty, slowMigrationVersion)
	}
}

func databaseConfigFromDSN(t *testing.T, dsn string) (platformdb.Config, string) {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	port, err := strconv.ParseUint(parsed.Port(), 10, 16)
	if err != nil {
		t.Fatalf("parse test database port: %v", err)
	}
	password, _ := parsed.User.Password()
	if password == "" {
		password = os.Getenv("ACCOUNTABLE_TEST_POSTGRES_PASSWORD")
	}
	return platformdb.Config{
		Host: parsed.Hostname(), Port: uint16(port), Name: parsed.Path[1:],
		User: parsed.User.Username(), Role: parsed.User.Username(), PasswordRef: secret.Ref("database.test_password"),
		TLSMode: platformdb.TLSDisable, ConnectTimeout: 5 * time.Second,
		StatementTimeout: 5 * time.Second, HealthCheckInterval: time.Second,
		MaxConnections: 2, Timezone: "UTC",
	}, password
}
