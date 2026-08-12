package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"
	"github.com/acctbl/accountable/gen/go/accountable/probe/v1/probev1connect"
	"github.com/acctbl/accountable/gen/go/accountable/system/v1/systemv1connect"
	"github.com/acctbl/accountable/internal/apierror"
	"github.com/acctbl/accountable/internal/appconfig"
	"github.com/acctbl/accountable/internal/bootstrap"
	"github.com/acctbl/accountable/internal/modules/probe"
	"github.com/acctbl/accountable/internal/modules/system"
	"github.com/acctbl/accountable/internal/platform/clock"
	"github.com/acctbl/accountable/internal/platform/secret"
	"github.com/acctbl/accountable/internal/platform/telemetry"
	"github.com/acctbl/accountable/internal/server"
)

const (
	readHeaderTimeout       = 5 * time.Second
	readTimeout             = 15 * time.Second
	writeTimeout            = appconfig.HTTPWriteTimeout
	idleTimeout             = 120 * time.Second
	shutdownTimeout         = 10 * time.Second
	ordinaryMessageMaxBytes = 1 << 20
	streamMessageMaxBytes   = 256 << 10
	uploadBodyMaxBytes      = 16 << 20
	maxHeaderBytes          = 64 << 10
	maxConnections          = 128
)

type config = appconfig.API

func loadConfig(args []string) (config, error) { return appconfig.LoadAPI(args) }

type readiness struct{ ready atomic.Bool }

func (r *readiness) Set(value bool) { r.ready.Store(value) }
func (r *readiness) IsReady() bool  { return r.ready.Load() }

func newAPIHandler(
	config config,
	ready *readiness,
	release telemetry.RuntimeRelease,
	signals *runtimeSignals,
) http.Handler {
	return newAPIHandlerWithSystem(
		config,
		ready,
		release,
		signals,
		system.NewSystemServer(clock.System{}, release.ReleaseID),
	)
}

func newAPIHandlerWithSystem(
	config config,
	ready *readiness,
	release telemetry.RuntimeRelease,
	signals *runtimeSignals,
	systemServer systemv1connect.SystemServiceHandler,
) http.Handler {
	mux := http.NewServeMux()
	identity := healthIdentity{
		ReleaseID:      release.ReleaseID,
		ServiceVersion: release.ServiceVersion,
		Configuration: telemetry.ConfigIdentity{
			SchemaVersion: config.Foundation.SchemaVersion,
			Revision:      config.Foundation.Revision,
			Fingerprint:   config.Foundation.Fingerprint,
		},
	}
	mux.HandleFunc("GET /_health/live", healthHandler(identity, func() bool { return true }))
	mux.HandleFunc("GET /_health/ready", healthHandler(identity, ready.IsReady))
	if signals != nil && signals.client != nil {
		mux.Handle("POST /telemetry/v1", clientTelemetryBoundary(clock.System{}, signals.client.Handle(config.AllowedOrigins)))
	}

	boundary := server.BoundaryInterceptor{}
	var recorder *telemetry.Recorder
	var safeLogger *telemetry.SafeLogger
	if signals != nil {
		recorder, safeLogger = signals.recorder, signals.logger
	}
	signalInterceptor := server.SignalInterceptor{Recorder: recorder, Logger: safeLogger}
	deadline := server.DeadlineInterceptor{
		Unary:  config.UnaryRPCDeadline,
		Stream: config.StreamRPCDeadline,
	}
	availability := server.AvailabilityInterceptor{Ready: ready.IsReady}
	recoverPanic := func(ctx context.Context, _ connect.Spec, _ http.Header, _ any) error {
		if safeLogger != nil {
			_ = safeLogger.Emit(ctx, slog.LevelError, "request.panic_recovered", nil)
		}
		return apierror.New(ctx, apierror.InternalFailure, "errors.internal", nil, 0)
	}
	ordinaryOptions := []connect.HandlerOption{
		connect.WithInterceptors(boundary, signalInterceptor, availability, deadline),
		connect.WithRecover(recoverPanic),
		connect.WithReadMaxBytes(ordinaryMessageMaxBytes),
		connect.WithSendMaxBytes(ordinaryMessageMaxBytes),
	}
	systemPath, systemHandler := systemv1connect.NewSystemServiceHandler(
		systemServer, ordinaryOptions...,
	)
	mux.Handle(systemPath, systemHandler)

	if config.ArchitectureProbe {
		probeOptions := []connect.HandlerOption{
			connect.WithInterceptors(boundary, signalInterceptor, availability, deadline),
			connect.WithRecover(recoverPanic),
			connect.WithReadMaxBytes(streamMessageMaxBytes),
			connect.WithSendMaxBytes(streamMessageMaxBytes),
		}
		probePath, probeHandler := probev1connect.NewArchitectureProbeServiceHandler(
			probe.ArchitectureProbeServer{}, probeOptions...,
		)
		mux.Handle(probePath, probeHandler)
	}

	handler := http.Handler(http.MaxBytesHandler(mux, uploadBodyMaxBytes))
	handler = trustedProxyMiddleware(config.TrustedProxies, handler)
	return credentialedCORSMiddleware(config.AllowedOrigins, handler)
}

// healthIdentity keeps release, artifact, and configuration identity separate.
// No field is derived from another field.
type healthIdentity struct {
	ReleaseID      string
	ServiceVersion string
	Configuration  telemetry.ConfigIdentity
}

type healthResponse struct {
	Status                     string `json:"status"`
	ReleaseID                  string `json:"release_id"`
	ServiceVersion             string `json:"service_version"`
	ConfigurationSchemaVersion int    `json:"configuration_schema_version"`
	ConfigurationRevision      string `json:"configuration_revision"`
	ConfigurationFingerprint   string `json:"configuration_fingerprint"`
	RequestID                  string `json:"request_id"`
	CorrelationID              string `json:"correlation_id"`
}

// healthHandler answers with the complete runtime identity. A health call starts
// one root journey, so its correlation ID equals its request ID.
func healthHandler(identity healthIdentity, healthy func() bool) http.HandlerFunc {
	return func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Content-Type", "application/json")
		requestID, err := telemetry.NewWorkID()
		if err != nil {
			response.Header().Set("Retry-After", "1")
			response.WriteHeader(http.StatusServiceUnavailable)
			_, _ = response.Write([]byte(`{"code":"unavailable","message_key":"errors.unavailable"}`))
			return
		}
		response.Header().Set(telemetry.HeaderRequestID, requestID)
		response.Header().Set(telemetry.HeaderCorrelationID, requestID)
		status := http.StatusOK
		payload := healthResponse{
			Status:                     "ok",
			ReleaseID:                  identity.ReleaseID,
			ServiceVersion:             identity.ServiceVersion,
			ConfigurationSchemaVersion: identity.Configuration.SchemaVersion,
			ConfigurationRevision:      identity.Configuration.Revision,
			ConfigurationFingerprint:   identity.Configuration.Fingerprint,
			RequestID:                  requestID,
			CorrelationID:              requestID,
		}
		if !healthy() {
			status = http.StatusServiceUnavailable
			payload.Status = "not_ready"
		}
		response.WriteHeader(status)
		_ = json.NewEncoder(response).Encode(payload)
	}
}

func trustedProxyMiddleware(trusted []*net.IPNet, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		host, _, err := net.SplitHostPort(request.RemoteAddr)
		ip := net.ParseIP(host)
		isTrusted := err == nil && ip != nil
		if isTrusted {
			isTrusted = false
			for _, network := range trusted {
				if network.Contains(ip) {
					isTrusted = true
					break
				}
			}
		}
		if !isTrusted {
			request.Header.Del("Forwarded")
			request.Header.Del("X-Forwarded-For")
			request.Header.Del("X-Forwarded-Host")
			request.Header.Del("X-Forwarded-Proto")
		}
		next.ServeHTTP(response, request)
	})
}

func credentialedCORSMiddleware(origins []string, next http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		allowed[origin] = struct{}{}
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		origin := request.Header.Get("Origin")
		if _, ok := allowed[origin]; origin != "" && ok {
			response.Header().Set("Access-Control-Allow-Origin", origin)
			response.Header().Set("Access-Control-Allow-Credentials", "true")
			response.Header().Set(
				"Access-Control-Expose-Headers",
				"X-Request-ID, X-Correlation-ID, Grpc-Status, Grpc-Message",
			)
			response.Header().Add("Vary", "Origin")
			if request.Method == http.MethodOptions {
				response.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
				response.Header().Set(
					"Access-Control-Allow-Headers",
					"Accept-Language, Connect-Protocol-Version, Connect-Timeout-Ms, Content-Type, X-Correlation-ID",
				)
				response.Header().Set("Access-Control-Max-Age", "600")
				response.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(response, request)
	})
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}
}

func serve(ctx context.Context, config config, release telemetry.RuntimeRelease) error {
	signals, err := startSignals(ctx, config, release)
	if err != nil {
		return err
	}
	served := bootstrapAndServe(
		ctx,
		config,
		release,
		signals,
		func(ctx context.Context, config bootstrap.Config) (ownedDependencySet, error) {
			return bootstrap.Build(ctx, config, signals.recorder)
		},
		serveWithDependencies,
	)
	// An unconfirmed flush is a telemetry loss, so it decides the exit status
	// when the request path itself finished cleanly.
	if flushed := signals.stop(context.WithoutCancel(ctx)); served == nil {
		return flushed
	}
	return served
}

type ownedDependencySet interface {
	dependencySet
	Close()
}

func bootstrapAndServe(
	ctx context.Context,
	config config,
	release telemetry.RuntimeRelease,
	signals *runtimeSignals,
	build func(context.Context, bootstrap.Config) (ownedDependencySet, error),
	serve func(context.Context, config, telemetry.RuntimeRelease, *runtimeSignals, dependencySet) error,
) error {
	dependencies, err := build(ctx, config.Foundation)
	if err != nil {
		return err
	}
	defer dependencies.Close()
	return serve(ctx, config, release, signals, dependencies)
}

type dependencySet interface {
	Ping(context.Context) error
}

func serveWithDependencies(
	ctx context.Context,
	config config,
	release telemetry.RuntimeRelease,
	signals *runtimeSignals,
	dependencies dependencySet,
) error {
	if signals.recorder != nil {
		activeRef, err := secret.ParseRef(config.ProofKeyActiveRef)
		if err != nil {
			return err
		}
		source, err := secret.NewSource(ctx, config.Foundation.Secrets)
		if err != nil {
			return err
		}
		values, err := source.ResolveBatch(ctx, []secret.Ref{activeRef})
		if err != nil {
			return err
		}
		activeValue, ok := values[activeRef]
		if !ok {
			return errors.New("the active proof key is unavailable")
		}
		standbyKey := ""
		if config.ProofKeyStandbyRef != "" {
			standbyRef, parseErr := secret.ParseRef(config.ProofKeyStandbyRef)
			if parseErr != nil {
				return parseErr
			}
			optionalSource, ok := source.(secret.OptionalSecretSource)
			if !ok {
				return errors.New("optional proof key resolution is unavailable")
			}
			standbyValue, present, resolveErr := optionalSource.ResolveOptional(ctx, standbyRef)
			if resolveErr != nil {
				return resolveErr
			}
			if present {
				standbyKey = string(standbyValue.Bytes())
			}
		}
		nonces, ok := dependencies.(proofNonceClaimer)
		if !ok {
			return errors.New("proof nonce storage is unavailable")
		}
		client, err := newClientTelemetryService(
			config.ClientTelemetry, config.PreviousClientTelemetry, release, signals.recorder, signals.logger,
			string(activeValue.Bytes()), standbyKey, nonces, clock.System{},
		)
		if err != nil {
			return err
		}
		signals.client = client
		defer func() {
			drainCtx, cancel := context.WithTimeout(context.Background(), clientTelemetryDrain)
			defer cancel()
			_ = client.Shutdown(drainCtx)
		}()
	}
	ready := &readiness{}
	srv := newHTTPServer(config.Addr, newAPIHandler(config, ready, release, signals))
	listener, listenErr := net.Listen("tcp", config.Addr)
	if listenErr != nil {
		return listenErr
	}
	limited := newLimitListener(listener, maxConnections)
	ready.Set(true)
	signals.ready(ctx)
	log.Printf("listening on %s", listener.Addr())

	errCh := make(chan error, 1)
	monitorCtx, stopMonitor := context.WithCancel(ctx)
	defer stopMonitor()
	go func() {
		var err error
		if config.TLSCertFile != "" || config.TLSKeyFile != "" {
			if config.TLSCertFile == "" || config.TLSKeyFile == "" {
				err = errors.New("tls_certificate_file and tls_private_key_file must be configured together")
			} else {
				err = srv.ServeTLS(limited, config.TLSCertFile, config.TLSKeyFile)
			}
		} else {
			err = srv.Serve(limited)
		}
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()
	go monitorDependencies(monitorCtx, config.Foundation, dependencies, ready)

	select {
	case err := <-errCh:
		ready.Set(false)
		shutdownErr := shutdownServer(srv)
		if err != nil {
			return err
		}
		return shutdownErr
	case <-ctx.Done():
		ready.Set(false)
		if err := shutdownServer(srv); err != nil {
			return err
		}
		return <-errCh
	}
}

// runtimeSignals owns the process telemetry for the API lifetime.
type runtimeSignals struct {
	providers *telemetry.Providers
	recorder  *telemetry.Recorder
	logger    *telemetry.SafeLogger
	client    *clientTelemetryService
}

// startSignals verifies runtime identity, starts every provider, and reports the
// starting and ready events. Telemetry is required in a managed environment and
// optional for a local run.
func startSignals(
	ctx context.Context,
	config config,
	release telemetry.RuntimeRelease,
) (*runtimeSignals, error) {
	identity := telemetry.RuntimeIdentity{
		Release: release,
		Config: telemetry.ConfigIdentity{
			SchemaVersion: config.Foundation.SchemaVersion,
			Revision:      config.Foundation.Revision,
			Fingerprint:   config.Foundation.Fingerprint,
		},
		Environment: config.Foundation.Environment,
		CellID:      config.Foundation.CellID,
		RuntimeRole: config.Foundation.RuntimeRole,
	}
	if !config.Foundation.Capabilities.Telemetry {
		return &runtimeSignals{}, nil
	}
	resource, err := telemetry.BuildProcessResource(identity)
	if err != nil {
		return nil, err
	}
	providers, err := telemetry.NewProviders(ctx, telemetry.ProviderOptions{
		Identity: identity,
		Config:   config.Foundation.Telemetry,
	})
	if err != nil {
		return nil, err
	}
	// The safe logger writes to stdout and appends the same record to the log
	// provider through the OpenTelemetry bridge.
	logger := telemetry.NewSafeLogger(telemetry.SafeLoggerOptions{
		Stdout:   os.Stdout,
		Resource: resource,
		Bridge:   slog.New(providers.BridgeHandler()),
	})
	recorder, err := telemetry.NewRecorder(providers)
	if err != nil {
		return nil, err
	}
	_ = logger.Emit(ctx, slog.LevelInfo, "runtime.starting", nil)
	return &runtimeSignals{providers: providers, recorder: recorder, logger: logger}, nil
}

// ready reports that the listener is open and the process accepts traffic.
func (signals *runtimeSignals) ready(ctx context.Context) {
	if signals.logger == nil {
		return
	}
	_ = signals.logger.Emit(ctx, slog.LevelInfo, "runtime.ready", nil)
}

// stop flushes every provider inside the bounded budget. An export failure is
// reported through the safe stdout path and never hides a request failure.
func (signals *runtimeSignals) stop(ctx context.Context) error {
	if signals.providers == nil {
		return nil
	}
	_ = signals.logger.Emit(ctx, slog.LevelInfo, "runtime.stopping", nil)
	if err := signals.providers.Shutdown(ctx); err != nil {
		_ = signals.logger.Emit(ctx, slog.LevelError, "telemetry.export_failed", nil)
		return err
	}
	return nil
}

func monitorDependencies(
	ctx context.Context,
	config bootstrap.Config,
	dependencies dependencySet,
	ready *readiness,
) {
	ticker := time.NewTicker(config.ReadinessProbeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, config.CheckTimeout)
			err := dependencies.Ping(pingCtx)
			cancel()
			if ctx.Err() != nil {
				return
			}
			ready.Set(err == nil)
		}
	}
}

func shutdownServer(server *http.Server) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return err
	}
	return nil
}

type limitListener struct {
	net.Listener
	sem chan struct{}
}

func newLimitListener(listener net.Listener, maximum int) *limitListener {
	return &limitListener{Listener: listener, sem: make(chan struct{}, maximum)}
}

func (l *limitListener) Accept() (net.Conn, error) {
	l.sem <- struct{}{}
	connection, err := l.Listener.Accept()
	if err != nil {
		<-l.sem
		return nil, err
	}
	return &limitedConn{Conn: connection, release: func() { <-l.sem }}, nil
}

type limitedConn struct {
	net.Conn
	release func()
	once    sync.Once
}

func (c *limitedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}
