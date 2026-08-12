package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/acctbl/accountable/internal/appconfig"
	"github.com/acctbl/accountable/internal/configfile"
	"github.com/acctbl/accountable/internal/platform/secret"
	"github.com/acctbl/accountable/internal/platform/telemetry"
	"github.com/jackc/pgx/v5"
)

const (
	apiRole                          = "accountable_api"
	databaseName                     = "accountable"
	databaseRootCA                   = "/etc/ssl/certs/aws-rds-global-bundle.pem"
	masterSecretDirectory            = "/run/accountable/secrets"
	masterRole                       = "accountable_admin"
	migrateRole                      = "accountable_migrate"
	masterPasswordRef     secret.Ref = "database-master-password"
	passwordRef           secret.Ref = "database-password"
)

var (
	awsRegionPattern     = regexp.MustCompile(`^[a-z]{2}(?:-gov)?-[a-z]+-\d$`)
	awsKMSKeyARNPattern  = regexp.MustCompile(`^arn:aws(?:-us-gov)?:kms:[a-z0-9-]+:[0-9]{12}:key/[A-Za-z0-9-]+$`)
	parameterPathPattern = regexp.MustCompile(`^(?:/[A-Za-z0-9._-]+){1,13}$`)
	cellIDPattern        = regexp.MustCompile(`^[a-z][a-z0-9-]{2,15}$`)
)

type config struct {
	apiSecrets     secret.Config
	databaseHost   string
	environment    string
	migrateSecrets secret.Config
	region         string
	cellID         string
	identity       telemetry.ConfigIdentity
	telemetry      telemetry.RuntimeConfig
}

type fileConfig struct {
	SchemaVersion int                  `toml:"schema_version"`
	Revision      string               `toml:"revision"`
	AWSRegion     string               `toml:"aws_region"`
	CellID        string               `toml:"cell_id"`
	DatabaseHost  string               `toml:"database_host"`
	Environment   string               `toml:"environment"`
	KMSKeyARN     string               `toml:"kms_key_arn"`
	ParameterRoot string               `toml:"parameter_root"`
	Telemetry     *telemetryFileConfig `toml:"telemetry"`
}

type telemetryFileConfig struct {
	CollectorEndpoint      string `toml:"collector_endpoint"`
	Protocol               string `toml:"protocol"`
	SamplingPolicy         string `toml:"sampling_policy"`
	RedactionPolicyVersion string `toml:"redaction_policy_version"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) (runErr error) {
	configPath, releasePath, err := appconfig.RuntimePaths(args)
	if err != nil {
		return err
	}
	release, err := telemetry.ReadRuntimeRelease(releasePath)
	if err != nil {
		return err
	}
	config, err := loadConfig([]string{"--config", configPath})
	if err != nil {
		return err
	}
	// Bootstrap holds the master credential and creates every role, so its
	// release identity is the one that matters most in an audit.
	signals, err := telemetry.StartRuntime(ctx, telemetry.RuntimeIdentity{
		Release:     release,
		Config:      config.identity,
		Environment: config.environment,
		CellID:      config.cellID,
		RuntimeRole: "bootstrap",
	}, config.telemetry, config.telemetry.CollectorEndpoint != "")
	if err != nil {
		return err
	}
	defer func() {
		runErr = errors.Join(runErr, signals.Stop(context.WithoutCancel(ctx)))
	}()
	apiStore, err := secret.NewStore(ctx, config.apiSecrets)
	if err != nil {
		return errors.New("API secret store is unavailable")
	}
	migrateStore, err := secret.NewStore(ctx, config.migrateSecrets)
	if err != nil {
		return errors.New("migration secret store is unavailable")
	}
	apiPassword, err := resolveDatabasePassword(ctx, apiStore)
	if err != nil {
		return errors.New("API database credential is unavailable")
	}
	migratePassword, err := resolveDatabasePassword(ctx, migrateStore)
	if err != nil {
		return errors.New("migration database credential is unavailable")
	}
	masterPassword, err := resolveMasterPassword(ctx)
	if err != nil {
		return errors.New("master database credential is unavailable")
	}
	connection, err := openMasterDatabase(ctx, config, masterPassword)
	if err != nil {
		return errors.New("master database connection is unavailable")
	}
	defer func() { _ = connection.Close(context.Background()) }()
	if err := initialiseDatabase(ctx, connection, apiPassword, migratePassword); err != nil {
		return err
	}
	fmt.Printf("database bootstrap: complete environment=%s\n", config.environment)
	signals.Emit(ctx, slog.LevelInfo, "runtime.task_completed", nil)
	return nil
}

func loadConfig(args []string) (config, error) {
	configPath, err := configfile.AbsolutePath(args)
	if err != nil {
		return config{}, err
	}
	var raw fileConfig
	fingerprint, err := configfile.DecodeWithFingerprint(configPath, &raw)
	if err != nil {
		return config{}, err
	}
	region := raw.AWSRegion
	databaseHost := raw.DatabaseHost
	environment := raw.Environment
	kmsKeyARN := raw.KMSKeyARN
	parameterRoot := raw.ParameterRoot
	if !awsRegionPattern.MatchString(region) || net.ParseIP(databaseHost) != nil || strings.TrimSpace(databaseHost) == "" ||
		(environment != "development" && environment != "staging" && environment != "production") ||
		!awsKMSKeyARNPattern.MatchString(kmsKeyARN) ||
		!parameterPathPattern.MatchString(parameterRoot) || path.Clean(parameterRoot) != parameterRoot {
		return config{}, errors.New("managed bootstrap environment is invalid")
	}
	secretConfig := func(parameterPath string) secret.Config {
		return secret.Config{
			Provider: secret.ProviderParameterStore, AWSRegion: region,
			ParameterPath: parameterPath, KMSKeyARN: kmsKeyARN,
		}
	}
	identity := telemetry.ConfigIdentity{
		SchemaVersion: raw.SchemaVersion,
		Revision:      raw.Revision,
		Fingerprint:   fingerprint,
	}
	if err := telemetry.ValidateConfigIdentity(identity); err != nil {
		return config{}, err
	}
	if !cellIDPattern.MatchString(raw.CellID) {
		return config{}, errors.New("managed bootstrap cell_id is invalid")
	}
	var runtimeTelemetry telemetry.RuntimeConfig
	if raw.Telemetry != nil {
		runtimeTelemetry = telemetry.RuntimeConfig{
			CollectorEndpoint:      raw.Telemetry.CollectorEndpoint,
			Protocol:               raw.Telemetry.Protocol,
			SamplingPolicy:         raw.Telemetry.SamplingPolicy,
			RedactionPolicyVersion: raw.Telemetry.RedactionPolicyVersion,
		}
		if err := telemetry.ValidateRuntimeConfig(runtimeTelemetry); err != nil {
			return config{}, err
		}
	}
	return config{
		apiSecrets: secretConfig(parameterRoot + "/api"), databaseHost: databaseHost, environment: environment,
		migrateSecrets: secretConfig(parameterRoot + "/migrate"), region: region,
		cellID: raw.CellID, identity: identity, telemetry: runtimeTelemetry,
	}, nil
}

func resolveMasterPassword(ctx context.Context) (secret.SecretValue, error) {
	source, err := secret.NewFileSecretSource(masterSecretDirectory)
	if err != nil {
		return secret.SecretValue{}, err
	}
	values, err := source.ResolveBatch(ctx, []secret.Ref{masterPasswordRef})
	if err != nil {
		return secret.SecretValue{}, err
	}
	return values[masterPasswordRef], nil
}

func resolveDatabasePassword(ctx context.Context, store secret.SecretStore) (secret.SecretValue, error) {
	values, err := store.ResolveOrCreateBatch(ctx, []secret.Ref{passwordRef}, func(secret.Ref) ([]byte, error) {
		value := make([]byte, 32)
		if _, err := rand.Read(value); err != nil {
			return nil, err
		}
		encoded := make([]byte, base64.RawURLEncoding.EncodedLen(len(value)))
		base64.RawURLEncoding.Encode(encoded, value)
		clear(value)
		return encoded, nil
	})
	if err != nil {
		return secret.SecretValue{}, err
	}
	value, ok := values[passwordRef]
	if !ok {
		return secret.SecretValue{}, errors.New("database credential is missing")
	}
	return value, nil
}

func openMasterDatabase(ctx context.Context, config config, masterPassword secret.SecretValue) (*pgx.Conn, error) {
	query := url.Values{
		"connect_timeout": []string{"5"},
		"sslmode":         []string{"verify-full"},
		"sslrootcert":     []string{databaseRootCA},
	}
	connectionURL := url.URL{
		Scheme:   "postgres",
		User:     url.User(masterRole),
		Host:     net.JoinHostPort(config.databaseHost, "5432"),
		Path:     databaseName,
		RawQuery: query.Encode(),
	}
	connectionConfig, err := pgx.ParseConfig(connectionURL.String())
	if err != nil {
		return nil, err
	}
	password := masterPassword.Bytes()
	connectionConfig.Password = string(password)
	clear(password)
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return pgx.ConnectConfig(connectCtx, connectionConfig)
}

func initialiseDatabase(
	ctx context.Context,
	connection *pgx.Conn,
	apiPassword secret.SecretValue,
	migratePassword secret.SecretValue,
) error {
	if err := ensureLoginRole(ctx, connection, apiRole, apiPassword); err != nil {
		return errors.New("API database role cannot be initialised")
	}
	if err := ensureLoginRole(ctx, connection, migrateRole, migratePassword); err != nil {
		return errors.New("migration database role cannot be initialised")
	}
	statements := []string{
		"GRANT CONNECT ON DATABASE " + pgx.Identifier{databaseName}.Sanitize() + " TO " + pgx.Identifier{apiRole}.Sanitize(),
		"GRANT CONNECT ON DATABASE " + pgx.Identifier{databaseName}.Sanitize() + " TO " + pgx.Identifier{migrateRole}.Sanitize(),
		"GRANT USAGE ON SCHEMA public TO " + pgx.Identifier{apiRole}.Sanitize(),
		"GRANT USAGE, CREATE ON SCHEMA public TO " + pgx.Identifier{migrateRole}.Sanitize(),
		"GRANT " + pgx.Identifier{migrateRole}.Sanitize() + " TO " + pgx.Identifier{masterRole}.Sanitize(),
		"ALTER DEFAULT PRIVILEGES FOR ROLE " + pgx.Identifier{migrateRole}.Sanitize() + " IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO " + pgx.Identifier{apiRole}.Sanitize(),
		"ALTER DEFAULT PRIVILEGES FOR ROLE " + pgx.Identifier{migrateRole}.Sanitize() + " IN SCHEMA public GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO " + pgx.Identifier{apiRole}.Sanitize(),
	}
	for _, statement := range statements {
		if _, err := connection.Exec(ctx, statement); err != nil {
			return errors.New("database grants cannot be initialised")
		}
	}
	return nil
}

func ensureLoginRole(ctx context.Context, connection *pgx.Conn, role string, password secret.SecretValue) error {
	var exists bool
	if err := connection.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", role).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if _, err := connection.Exec(ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN"); err != nil {
			return err
		}
	}
	passwordBytes := password.Bytes()
	defer clear(passwordBytes)
	var statement string
	if err := connection.QueryRow(
		ctx,
		"SELECT format('ALTER ROLE %I WITH LOGIN PASSWORD %L', $1::text, $2::text)",
		role,
		string(passwordBytes),
	).Scan(&statement); err != nil {
		return err
	}
	_, err := connection.Exec(ctx, statement)
	return err
}
