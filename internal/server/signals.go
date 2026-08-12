package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/acctbl/accountable/internal/apierror"
	"github.com/acctbl/accountable/internal/platform/clock"
	"github.com/acctbl/accountable/internal/platform/telemetry"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// SignalInterceptor records the span, the metrics, and the safe log record for
// one Connect RPC. It runs inside BoundaryInterceptor, so the request and
// correlation identity already exist.
type SignalInterceptor struct {
	Recorder *telemetry.Recorder
	Logger   *telemetry.SafeLogger
	Clock    clock.Clock
}

func (interceptor SignalInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		ctx, finish, err := interceptor.start(ctx, req.Spec().Procedure, req.Header(), true)
		if err != nil {
			return nil, err
		}
		response, nextErr := next(ctx, req)
		finish(nextErr)
		return response, nextErr
	}
}

func (interceptor SignalInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (interceptor SignalInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		ctx, finish, err := interceptor.start(ctx, conn.Spec().Procedure, conn.RequestHeader(), false)
		if err != nil {
			return err
		}
		nextErr := next(ctx, conn)
		finish(nextErr)
		return nextErr
	}
}

// start opens the server span and returns the completion function. Public trace
// context becomes a link only, so a caller can never choose the server parent or
// the server sample.
func (interceptor SignalInterceptor) start(
	ctx context.Context,
	procedure string,
	header http.Header,
	unary bool,
) (context.Context, func(error), error) {
	if interceptor.Recorder == nil {
		return ctx, func(error) {}, nil
	}
	service, method := splitProcedure(procedure)
	routeClass := RouteClass(procedure)
	identity, _ := telemetry.WorkIdentityFromContext(ctx)
	identity, identityErr := telemetry.WithOperation(identity, "")
	if identityErr != nil {
		// No substitute identity exists, so the request cannot proceed.
		if interceptor.Logger != nil {
			_ = interceptor.Logger.Emit(ctx, slog.LevelError, "identity.generation_failed", nil)
		}
		return ctx, nil, apierror.New(ctx, apierror.Unavailable, "errors.unavailable", nil, time.Second)
	}
	ctx = telemetry.ContextWithWorkIdentity(ctx, identity)
	attributes, attributeErr := telemetry.SpanAttributes(identity, map[string]string{
		"route_class": routeClass,
		"rpc_service": service,
		"rpc_method":  method,
	})
	if attributeErr != nil {
		attributes = nil
	}
	ctx, span := interceptor.Recorder.StartServerSpan(ctx, procedure, PublicTraceLink(header), attributes)
	interceptor.Recorder.RequestStarted(ctx, routeClass)
	started := interceptor.now()

	return ctx, func(err error) {
		outcome, category := Outcome(err)
		if err != nil {
			span.SetStatus(codes.Error, category)
		}
		span.End()
		_ = interceptor.Recorder.RequestFinished(ctx, telemetry.ServerRequest{
			RouteClass:    routeClass,
			RPCService:    service,
			RPCMethod:     method,
			Outcome:       outcome,
			ErrorCategory: category,
			Duration:      interceptor.now().Sub(started),
			Eligible:      unary && routeClass == "system" && !errors.Is(err, context.Canceled),
		})
		if interceptor.Logger == nil {
			return
		}
		event := "request.completed"
		level := slog.LevelInfo
		if err != nil {
			event = "request.failed"
			level = slog.LevelError
		}
		_ = interceptor.Logger.Emit(ctx, level, event, map[string]string{
			"route_class":    routeClass,
			"rpc_service":    service,
			"rpc_method":     method,
			"outcome":        outcome,
			"error_category": category,
		})
	}, nil
}

func (interceptor SignalInterceptor) now() time.Time {
	if interceptor.Clock == nil {
		return clock.System{}.Now()
	}
	return interceptor.Clock.Now()
}

// PublicTraceLink reports the inbound W3C trace context as a link candidate. The
// server never adopts it as a parent, and public sampling flags never control a
// server sample. tracestate and baggage are discarded.
func PublicTraceLink(header http.Header) oteltrace.SpanContext {
	traceParent := header.Get("traceparent")
	parts := strings.Split(traceParent, "-")
	if len(parts) != 4 || parts[0] != "00" {
		return oteltrace.SpanContext{}
	}
	traceID, err := oteltrace.TraceIDFromHex(parts[1])
	if err != nil {
		return oteltrace.SpanContext{}
	}
	spanID, err := oteltrace.SpanIDFromHex(parts[2])
	if err != nil {
		return oteltrace.SpanContext{}
	}
	return oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
		Remote:  true,
	})
}

// RouteClass reports the bounded route class for one registered procedure. An
// unregistered procedure never contributes its raw path.
func RouteClass(procedure string) string {
	switch {
	case strings.HasPrefix(procedure, "/accountable.system.v1.SystemService/"):
		return "system"
	case strings.HasPrefix(procedure, "/accountable.probe.v1.ArchitectureProbeService/"):
		return "architecture_probe"
	default:
		return "unregistered"
	}
}

// Outcome maps one handler result to the bounded outcome and error category.
func Outcome(err error) (outcome string, category string) {
	if err == nil {
		return "success", "none"
	}
	problem := apierror.CategoryOf(err)
	if problem == "" {
		return "failure", "unknown"
	}
	// A caller-caused rejection is a correct server answer, so only a
	// server-caused category counts as a failed request.
	switch problem {
	case apierror.InvalidInput, apierror.Unauthenticated, apierror.Forbidden,
		apierror.NotFound, apierror.AlreadyExists, apierror.FailedPrecondition,
		apierror.StaleVersion, apierror.Conflict, apierror.OutOfRange:
		return "success", string(problem)
	default:
		return "failure", string(problem)
	}
}

func splitProcedure(procedure string) (service string, method string) {
	trimmed := strings.TrimPrefix(procedure, "/")
	separator := strings.LastIndex(trimmed, "/")
	if separator < 0 {
		return "unregistered", "unregistered"
	}
	return trimmed[:separator], trimmed[separator+1:]
}
