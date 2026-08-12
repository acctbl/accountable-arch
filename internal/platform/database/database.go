package database

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/acctbl/accountable/internal/platform/clock"
	"github.com/acctbl/accountable/internal/platform/secret"
	"github.com/acctbl/accountable/internal/platform/telemetry"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

const (
	// MinimumSchemaVersion and MaximumSchemaVersion are compiled into the binary.
	// Operators cannot widen this compatibility window through configuration.
	MinimumSchemaVersion int64 = 20260801000000
	MaximumSchemaVersion int64 = 20260811000000
)

var (
	ErrDatabaseUnavailable = errors.New("database is unavailable")
	ErrDatabaseTimezone    = errors.New("database session timezone is not UTC")
	ErrDatabaseSession     = errors.New("database session settings are unsafe")
	ErrDatabaseClockSkew   = errors.New("database clock exceeds the configured skew")
	ErrSchemaBehind        = errors.New("database schema is behind the supported window")
	ErrSchemaDirty         = errors.New("database schema migration is incomplete")
	ErrSchemaAhead         = errors.New("database schema is ahead of the supported window")
)

type Database struct {
	pool   *pgxpool.Pool
	config Config
}

// OpenDatabase opens the pool. A recorder attaches the query tracer, so every
// database operation carries its span and its bounded metric point.
func OpenDatabase(
	ctx context.Context,
	config Config,
	source secret.SecretSource,
	recorder *telemetry.Recorder,
) (*Database, error) {
	connectCtx, cancel := context.WithTimeout(ctx, config.ConnectTimeout)
	defer cancel()

	values, err := source.ResolveBatch(connectCtx, []secret.Ref{config.PasswordRef})
	if err != nil {
		return nil, ErrDatabaseUnavailable
	}
	password, ok := values[config.PasswordRef]
	if !ok {
		return nil, ErrDatabaseUnavailable
	}
	poolConfig, err := databasePoolConfig(config, password)
	if err != nil {
		return nil, errors.New("database configuration is invalid")
	}
	if recorder != nil {
		poolConfig.ConnConfig.Tracer = SignalTracer{Recorder: recorder}
	}
	pool, err := pgxpool.NewWithConfig(connectCtx, poolConfig)
	if err != nil {
		return nil, ErrDatabaseUnavailable
	}
	database := &Database{pool: pool, config: config}
	if err := database.Check(connectCtx); err != nil {
		pool.Close()
		return nil, err
	}
	return database, nil
}

func databasePoolConfig(config Config, password secret.SecretValue) (*pgxpool.Config, error) {
	query := url.Values{"sslmode": []string{config.TLSMode}}
	if config.TLSRootCAFile != "" {
		query.Set("sslrootcert", config.TLSRootCAFile)
	}
	connectionURL := url.URL{
		Scheme:   "postgres",
		User:     url.User(config.User),
		Host:     net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port))),
		Path:     config.Name,
		RawQuery: query.Encode(),
	}
	poolConfig, err := pgxpool.ParseConfig(connectionURL.String())
	if err != nil {
		return nil, err
	}
	secretBytes := password.Bytes()
	poolConfig.ConnConfig.Password = string(secretBytes)
	clear(secretBytes)
	poolConfig.ConnConfig.ConnectTimeout = config.ConnectTimeout
	poolConfig.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(config.StatementTimeout.Milliseconds(), 10)
	poolConfig.MaxConns = config.MaxConnections
	poolConfig.MaxConnIdleTime = 5 * time.Minute
	poolConfig.MaxConnLifetime = time.Hour
	poolConfig.HealthCheckPeriod = config.HealthCheckInterval
	poolConfig.AfterConnect = databaseSessionInitializer(config.Role)
	return poolConfig, nil
}

func OpenMigrationDatabase(ctx context.Context, config Config, source secret.SecretSource) (*sql.DB, error) {
	values, err := source.ResolveBatch(ctx, []secret.Ref{config.PasswordRef})
	if err != nil {
		return nil, ErrDatabaseUnavailable
	}
	password, ok := values[config.PasswordRef]
	if !ok {
		return nil, ErrDatabaseUnavailable
	}
	poolConfig, err := databasePoolConfig(config, password)
	if err != nil {
		return nil, errors.New("database configuration is invalid")
	}
	database := stdlib.OpenDB(
		*poolConfig.ConnConfig,
		stdlib.OptionAfterConnect(databaseSessionInitializer(config.Role)),
	)
	database.SetMaxOpenConns(int(config.MaxConnections))
	database.SetMaxIdleConns(int(config.MaxConnections))
	connectCtx, cancel := context.WithTimeout(ctx, config.ConnectTimeout)
	defer cancel()
	if err := database.PingContext(connectCtx); err != nil {
		_ = database.Close()
		return nil, ErrDatabaseUnavailable
	}
	return database, nil
}

func databaseSessionInitializer(role string) func(context.Context, *pgx.Conn) error {
	return func(ctx context.Context, connection *pgx.Conn) error {
		if _, err := connection.Exec(ctx, `SET TIME ZONE 'UTC'`); err != nil {
			return err
		}
		if _, err := connection.Exec(ctx, `SET search_path TO public`); err != nil {
			return err
		}
		_, err := connection.Exec(ctx, "SET ROLE "+pgx.Identifier{role}.Sanitize())
		return err
	}
}

func (d *Database) Check(ctx context.Context) error {
	connection, err := d.pool.Acquire(ctx)
	if err != nil {
		return ErrDatabaseUnavailable
	}
	defer connection.Release()

	var timezone, sessionUser, currentRole string
	var statementTimeoutMatches, tlsActive bool
	err = connection.QueryRow(ctx, `
SELECT
    current_setting('TimeZone'),
    session_user,
    current_role,
    current_setting('statement_timeout')::interval = ($1::bigint * interval '1 millisecond'),
    COALESCE((SELECT ssl FROM pg_stat_ssl WHERE pid = pg_backend_pid()), FALSE)`,
		d.config.StatementTimeout.Milliseconds(),
	).Scan(&timezone, &sessionUser, &currentRole, &statementTimeoutMatches, &tlsActive)
	if err != nil {
		return ErrDatabaseUnavailable
	}
	if timezone != "UTC" {
		return ErrDatabaseTimezone
	}
	wantTLS := d.config.TLSMode == TLSVerifyFull
	if sessionUser != d.config.User || currentRole != d.config.Role || !statementTimeoutMatches || tlsActive != wantTLS {
		return ErrDatabaseSession
	}

	var exists bool
	if err := connection.QueryRow(ctx, `SELECT to_regclass('public.accountable_schema_state') IS NOT NULL`).Scan(&exists); err != nil {
		return ErrDatabaseUnavailable
	}
	if !exists {
		return ErrSchemaBehind
	}
	var version int64
	var dirty bool
	if err := connection.QueryRow(ctx, `SELECT version, dirty FROM public.accountable_schema_state WHERE singleton = TRUE`).Scan(&version, &dirty); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSchemaBehind
		}
		return ErrDatabaseUnavailable
	}
	return ValidateSchemaState(version, dirty)
}

func (d *Database) Ping(ctx context.Context) error {
	if err := d.pool.Ping(ctx); err != nil {
		return ErrDatabaseUnavailable
	}
	return nil
}

func (d *Database) Close() { d.pool.Close() }

func (d *Database) ClaimProofNonce(ctx context.Context, nonce string, expiresAt time.Time) (bool, error) {
	var claimed bool
	err := d.pool.QueryRow(ctx, `
WITH expired AS (
    DELETE FROM public.accountable_proof_nonces WHERE expires_at < clock_timestamp()
), claimed AS (
    INSERT INTO public.accountable_proof_nonces (nonce, expires_at)
    VALUES ($1, $2)
    ON CONFLICT DO NOTHING
    RETURNING TRUE
)
SELECT COALESCE((SELECT TRUE FROM claimed), FALSE)`, nonce, expiresAt.UTC()).Scan(&claimed)
	if err != nil {
		return false, ErrDatabaseUnavailable
	}
	return claimed, nil
}

func (d *Database) CheckClock(ctx context.Context, source clock.Clock, maximumSkew time.Duration) error {
	before := source.Now()
	var databaseTime time.Time
	if err := d.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&databaseTime); err != nil {
		return ErrDatabaseUnavailable
	}
	after := source.Now()
	midpoint := before.Add(after.Sub(before) / 2)
	skew := databaseTime.UTC().Sub(midpoint)
	if skew < 0 {
		skew = -skew
	}
	if skew > maximumSkew {
		return ErrDatabaseClockSkew
	}
	return nil
}

func ValidateSchemaState(version int64, dirty bool) error {
	if dirty {
		return ErrSchemaDirty
	}
	if version < MinimumSchemaVersion {
		return ErrSchemaBehind
	}
	if version > MaximumSchemaVersion {
		return ErrSchemaAhead
	}
	return nil
}
