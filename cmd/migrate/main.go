package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/acctbl/accountable/internal/appconfig"
	"github.com/acctbl/accountable/internal/bootstrap"
	"github.com/acctbl/accountable/internal/migration"
	"github.com/acctbl/accountable/internal/platform/database"
	"github.com/acctbl/accountable/internal/platform/secret"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	config, err := appconfig.LoadFoundation(args, bootstrap.RuntimeRoleMigrate)
	if err != nil {
		return err
	}
	if !config.Capabilities.Secrets || !config.Capabilities.Postgres {
		return fmt.Errorf("migrate requires secrets and postgres capabilities")
	}
	fmt.Printf(
		"configuration: environment=%s deployment_mode=%s cell=%s role=%s revision=%s fingerprint=%s\n",
		config.Environment, config.DeploymentMode, config.CellID, config.RuntimeRole, config.Revision,
		config.Fingerprint,
	)
	source, err := secret.NewSource(ctx, config.Secrets)
	if err != nil {
		return err
	}
	sqlDB, err := database.OpenMigrationDatabase(ctx, config.Database, source)
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()
	return migration.Run(ctx, sqlDB)
}
