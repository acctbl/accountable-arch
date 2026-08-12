package database

import (
	"context"
	"strings"
	"time"

	"github.com/acctbl/accountable/internal/platform/clock"
	"github.com/acctbl/accountable/internal/platform/telemetry"
	"github.com/jackc/pgx/v5"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// operationClasses maps the leading SQL verb to its bounded class. A statement
// that is not in this table is never classified by its text.
var operationClasses = map[string]string{
	"select": "read",
	"insert": "write",
	"update": "write",
	"delete": "write",
	"begin":  "transaction",
	"commit": "transaction",
}

// SignalTracer records one span and one metric point for each database query.
// It never records SQL text or SQL arguments, only a bounded operation class.
type SignalTracer struct {
	Recorder *telemetry.Recorder
	Clock    clock.Clock
}

type databaseSpanKey struct{}

type databaseSpanState struct {
	span    oteltrace.Span
	class   string
	started time.Time
}

func (tracer SignalTracer) TraceQueryStart(
	ctx context.Context,
	_ *pgx.Conn,
	data pgx.TraceQueryStartData,
) context.Context {
	if tracer.Recorder == nil {
		return ctx
	}
	class := OperationClass(data.SQL)
	identity, _ := telemetry.WorkIdentityFromContext(ctx)
	attributes, err := telemetry.SpanAttributes(identity, map[string]string{"operation_class": class})
	if err != nil {
		attributes = nil
	}
	ctx, span := tracer.Recorder.StartInternalSpan(ctx, "postgresql."+class, oteltrace.SpanKindClient, attributes)
	return context.WithValue(ctx, databaseSpanKey{}, &databaseSpanState{
		span:    span,
		class:   class,
		started: tracer.now(),
	})
}

func (tracer SignalTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	state, ok := ctx.Value(databaseSpanKey{}).(*databaseSpanState)
	if !ok || tracer.Recorder == nil {
		return
	}
	outcome, category := "success", "none"
	if data.Err != nil {
		outcome, category = "failure", "internal_failure"
	}
	state.span.End()
	_ = tracer.Recorder.DatabaseFinished(ctx, telemetry.DependencyOperation{
		Class:         state.class,
		Outcome:       outcome,
		ErrorCategory: category,
		Duration:      tracer.now().Sub(state.started),
	})
}

func (tracer SignalTracer) now() time.Time {
	if tracer.Clock == nil {
		return clock.System{}.Now()
	}
	return tracer.Clock.Now()
}

// OperationClass reports the bounded class of one statement. The statement text
// never leaves this function.
func OperationClass(statement string) string {
	verb, _, _ := strings.Cut(strings.TrimSpace(statement), " ")
	if class, registered := operationClasses[strings.ToLower(verb)]; registered {
		return class
	}
	return "unknown"
}
