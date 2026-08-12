package server

import (
	"context"

	"connectrpc.com/connect"
	"github.com/acctbl/accountable/internal/apierror"
	"github.com/acctbl/accountable/internal/platform/telemetry"
)

// BoundaryInterceptor owns the public work identity of one request. The public
// journey header is the correlation ID; an OpenTelemetry trace ID is never a
// public header and never controls server identity.
type BoundaryInterceptor struct{}

func (BoundaryInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		ctx, identity, err := requestIdentity(ctx, req.Header().Get(telemetry.HeaderCorrelationID))
		if err != nil {
			return nil, err
		}
		response, nextErr := next(ctx, req)
		if response != nil {
			setIdentityHeaders(response.Header(), identity)
		}
		return response, apierror.EnsureSafe(ctx, nextErr)
	}
}

func (BoundaryInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (BoundaryInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		ctx, identity, err := requestIdentity(ctx, conn.RequestHeader().Get(telemetry.HeaderCorrelationID))
		if err != nil {
			return err
		}
		setIdentityHeaders(conn.ResponseHeader(), identity)
		return apierror.EnsureSafe(ctx, next(ctx, conn))
	}
}

// requestIdentity makes the request identity and continues an inbound journey
// only when the inbound correlation ID is a canonical UUIDv7. A failed identity
// generation never substitutes another identity.
func requestIdentity(
	ctx context.Context,
	inboundCorrelationID string,
) (context.Context, apierror.Identity, error) {
	work, err := telemetry.StartRequestIdentity(inboundCorrelationID)
	if err != nil {
		return ctx, apierror.Identity{}, apierror.New(ctx, apierror.Unavailable, "errors.unavailable", nil, 0)
	}
	identity := apierror.Identity{RequestID: work.RequestID, CorrelationID: work.CorrelationID}
	ctx = telemetry.ContextWithWorkIdentity(ctx, work)
	return apierror.WithIdentity(ctx, identity), identity, nil
}

func setIdentityHeaders(header interface{ Set(string, string) }, identity apierror.Identity) {
	if identity.RequestID != "" {
		header.Set(telemetry.HeaderRequestID, identity.RequestID)
	}
	if identity.CorrelationID != "" {
		header.Set(telemetry.HeaderCorrelationID, identity.CorrelationID)
	}
}
