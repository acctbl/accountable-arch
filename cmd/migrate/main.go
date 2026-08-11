package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/acctbl/accountable/internal/appconfig"
	"github.com/acctbl/accountable/internal/bootstrap"
	"github.com/acctbl/accountable/internal/migration"
	"github.com/acctbl/accountable/internal/platform/database"
	"github.com/acctbl/accountable/internal/platform/secret"
	"github.com/acctbl/accountable/internal/platform/telemetry"
	"github.com/pressly/goose/v3"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) (runErr error) {
	config, release, err := appconfig.LoadRuntime(args, bootstrap.RuntimeRoleMigrate)
	if err != nil {
		return err
	}
	if !config.Capabilities.Secrets || !config.Capabilities.Postgres {
		return fmt.Errorf("migrate requires secrets and postgres capabilities")
	}
	signals, err := telemetry.StartRuntime(
		ctx,
		runtimeIdentity(config, release),
		config.Telemetry,
		config.Capabilities.Telemetry,
	)
	if err != nil {
		return err
	}
	defer func() {
		runErr = errors.Join(runErr, signals.Stop(context.WithoutCancel(ctx)))
	}()
	if !config.Capabilities.Telemetry {
		goose.SetLogger(goose.NopLogger())
	}
	source, err := secret.NewSource(ctx, config.Secrets)
	if err != nil {
		return err
	}
	sqlDB, err := database.OpenMigrationDatabase(ctx, config.Database, source)
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()
	if err := migration.Run(ctx, sqlDB); err != nil {
		return err
	}
	if !config.Capabilities.Telemetry {
		fmt.Println("migrate: OK")
	}
	signals.Emit(ctx, slog.LevelInfo, "runtime.task_completed", nil)
	return nil
}

// runtimeIdentity builds the resource identity for one one-off runtime.
func runtimeIdentity(config bootstrap.Config, release telemetry.RuntimeRelease) telemetry.RuntimeIdentity {
	return telemetry.RuntimeIdentity{
		Release: release,
		Config: telemetry.ConfigIdentity{
			SchemaVersion: config.SchemaVersion,
			Revision:      config.Revision,
			Fingerprint:   config.Fingerprint,
		},
		Environment: config.Environment,
		CellID:      config.CellID,
		RuntimeRole: config.RuntimeRole,
	}
}
