package probe

import (
	"context"
	"net/http"
	"time"

	"connectrpc.com/connect"
	probev1 "github.com/acctbl/accountable/gen/go/accountable/probe/v1"
	"github.com/acctbl/accountable/internal/apierror"
)

const probeCookieName = "accountable_architecture_probe"

type ArchitectureProbeServer struct{}

func (ArchitectureProbeServer) Correlate(
	ctx context.Context,
	_ *connect.Request[probev1.CorrelateRequest],
) (*connect.Response[probev1.CorrelateResponse], error) {
	identity := apierror.IdentityFromContext(ctx)
	return connect.NewResponse(&probev1.CorrelateResponse{
		RequestId: identity.RequestID,
		TraceId:   identity.CorrelationID,
	}), nil
}

func (ArchitectureProbeServer) CookieRoundTrip(
	ctx context.Context,
	req *connect.Request[probev1.CookieRoundTripRequest],
) (*connect.Response[probev1.CookieRoundTripResponse], error) {
	identity := apierror.IdentityFromContext(ctx)
	request := &http.Request{Header: req.Header()}
	cookie, cookieErr := request.Cookie(probeCookieName)
	received := cookieErr == nil && cookie.Value == "present"

	response := connect.NewResponse(&probev1.CookieRoundTripResponse{
		CookieReceived: received,
		RequestId:      identity.RequestID,
		TraceId:        identity.CorrelationID,
	})
	if req.Msg.GetSetCookie() {
		response.Header().Add("Set-Cookie", (&http.Cookie{
			Name:  probeCookieName,
			Value: "present",
			// The edge serves this API under a public /api prefix that the origin
			// never sees, so an origin-space path would never match on the way back.
			Path:     "/",
			MaxAge:   60,
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		}).String())
	}
	return response, nil
}

func (ArchitectureProbeServer) Fail(
	ctx context.Context,
	req *connect.Request[probev1.FailRequest],
) (*connect.Response[probev1.FailResponse], error) {
	switch req.Msg.GetKind() {
	case probev1.FailureKind_FAILURE_KIND_INVALID_INPUT:
		return nil, apierror.New(ctx, apierror.InvalidInput, "errors.invalidInput", []apierror.FieldViolation{{
			FieldPath: "kind", Code: "invalid_failure_kind",
		}}, 0)
	case probev1.FailureKind_FAILURE_KIND_UNAVAILABLE:
		return nil, apierror.New(ctx, apierror.Unavailable, "errors.unavailable", nil, time.Second)
	case probev1.FailureKind_FAILURE_KIND_INTERNAL_FAILURE:
		return nil, apierror.New(ctx, apierror.InternalFailure, "errors.internal", nil, 0)
	default:
		return nil, apierror.New(ctx, apierror.InvalidInput, "errors.invalidInput", []apierror.FieldViolation{{
			FieldPath: "kind", Code: "required",
		}}, 0)
	}
}

func (ArchitectureProbeServer) Wait(
	ctx context.Context,
	req *connect.Request[probev1.WaitRequest],
) (*connect.Response[probev1.WaitResponse], error) {
	delay := 10 * time.Second
	if req.Msg.GetDelay() != nil {
		delay = req.Msg.GetDelay().AsDuration()
	}
	if delay <= 0 || delay > 30*time.Second {
		return nil, apierror.New(ctx, apierror.InvalidInput, "errors.invalidInput", nil, 0)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		identity := apierror.IdentityFromContext(ctx)
		return connect.NewResponse(&probev1.WaitResponse{
			RequestId: identity.RequestID,
			TraceId:   identity.CorrelationID,
		}), nil
	}
}

func (ArchitectureProbeServer) Stream(
	ctx context.Context,
	req *connect.Request[probev1.StreamRequest],
	stream *connect.ServerStream[probev1.StreamResponse],
) error {
	count := req.Msg.GetCount()
	if count == 0 || count > 20 {
		return apierror.New(ctx, apierror.InvalidInput, "errors.invalidInput", nil, 0)
	}
	interval := 25 * time.Millisecond
	if req.Msg.GetInterval() != nil {
		interval = req.Msg.GetInterval().AsDuration()
	}
	if interval < 0 || interval > time.Second {
		return apierror.New(ctx, apierror.InvalidInput, "errors.invalidInput", nil, 0)
	}
	identity := apierror.IdentityFromContext(ctx)
	for sequence := uint32(1); sequence <= count; sequence++ {
		if sequence > 1 && interval > 0 {
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		if err := stream.Send(&probev1.StreamResponse{
			Sequence:  sequence,
			RequestId: identity.RequestID,
			TraceId:   identity.CorrelationID,
		}); err != nil {
			return err
		}
	}
	return nil
}
