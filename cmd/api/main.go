package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/acctbl/accountable/internal/appconfig"
	"github.com/acctbl/accountable/internal/platform/telemetry"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	configPath, releasePath, err := appconfig.RuntimePaths(args)
	if err != nil {
		return err
	}
	config, err := loadConfig([]string{"--config", configPath})
	if err != nil {
		return err
	}
	release, err := telemetry.ReadRuntimeRelease(releasePath)
	if err != nil {
		return err
	}
	return serve(ctx, config, release)
}
