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
	"github.com/acctbl/accountable/internal/platform/telemetry"
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
	config, release, err := appconfig.LoadRuntime(args, "")
	if err != nil {
		return err
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
	if _, err := bootstrap.Preflight(ctx, config); err != nil {
		signals.Emit(ctx, slog.LevelError, "runtime.configuration_failed", nil)
		return err
	}
	if !config.Capabilities.Telemetry {
		fmt.Println("preflight: safe")
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
