package telemetry

import (
	"context"
	"log/slog"
	"os"
)

// RuntimeSignals is the process telemetry that every managed runtime owns: the
// verified resource identity, the providers, and the safe stdout writer.
// Managed runtimes (telemetry enabled) always mirror contract events to stdout.
// Local runtimes with telemetry disabled keep the same Emit call sites but write
// nothing, so developer terminals stay quiet.
type RuntimeSignals struct {
	providers *Providers
	recorder  *Recorder
	logger    *SafeLogger
}

// StartRuntime verifies the runtime identity, starts the providers, and reports
// that the process is starting. Telemetry enabled is the managed default.
func StartRuntime(
	ctx context.Context,
	identity RuntimeIdentity,
	config RuntimeConfig,
	enabled bool,
) (*RuntimeSignals, error) {
	resource, err := BuildProcessResource(identity)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return &RuntimeSignals{}, nil
	}
	providers, err := NewProviders(ctx, ProviderOptions{Identity: identity, Config: config})
	if err != nil {
		return nil, err
	}
	recorder, err := NewRecorder(providers)
	if err != nil {
		return nil, err
	}
	signals := &RuntimeSignals{
		providers: providers,
		recorder:  recorder,
		logger: NewSafeLogger(SafeLoggerOptions{
			Stdout:   os.Stdout,
			Resource: resource,
			Bridge:   slog.New(providers.BridgeHandler()),
		}),
	}
	signals.Emit(ctx, slog.LevelInfo, "runtime.starting", nil)
	return signals, nil
}

func (signals *RuntimeSignals) Recorder() *Recorder { return signals.recorder }

func (signals *RuntimeSignals) Emit(
	ctx context.Context,
	level slog.Level,
	eventName string,
	attributes map[string]string,
) {
	if signals.logger == nil {
		return
	}
	_ = signals.logger.Emit(ctx, level, eventName, attributes)
}

// Stop flushes every provider inside the bounded budget. An unconfirmed flush is
// a telemetry loss, so it is reported and returned for the exit status.
func (signals *RuntimeSignals) Stop(ctx context.Context) error {
	if signals.providers == nil {
		return nil
	}
	signals.Emit(ctx, slog.LevelInfo, "runtime.stopping", nil)
	if err := signals.providers.Shutdown(ctx); err != nil {
		signals.Emit(ctx, slog.LevelError, "telemetry.export_failed", nil)
		return err
	}
	return nil
}
