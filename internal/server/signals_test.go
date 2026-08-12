package server_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/acctbl/accountable/internal/apierror"
	"github.com/acctbl/accountable/internal/server"
)

func TestPublicTraceLinkNeverBecomesTheServerParent(t *testing.T) {
	t.Parallel()

	header := http.Header{}
	header.Set("traceparent", "00-11111111111111111111111111111111-2222222222222222-01")
	header.Set("tracestate", "vendor=value")
	header.Set("baggage", "user=1")

	link := server.PublicTraceLink(header)
	if !link.IsValid() {
		t.Fatal("a valid public traceparent must become a link candidate")
	}
	if !link.IsRemote() {
		t.Fatal("public trace context must stay remote")
	}
	// A public sampling flag never controls a server sample.
	if link.IsSampled() {
		t.Fatal("public sampling flags must not reach the server sampler")
	}
	if link.TraceState().Len() != 0 {
		t.Fatal("public tracestate must be discarded")
	}

	for name, value := range map[string]string{
		"absent":         "",
		"wrong version":  "01-11111111111111111111111111111111-2222222222222222-01",
		"short trace":    "00-1111-2222222222222222-01",
		"malformed span": "00-11111111111111111111111111111111-zz-01",
	} {
		t.Run(name, func(t *testing.T) {
			rejected := http.Header{}
			rejected.Set("traceparent", value)
			if server.PublicTraceLink(rejected).IsValid() {
				t.Fatalf("%s traceparent produced a link", name)
			}
		})
	}
}

func TestRouteClassNeverLeaksAnUnregisteredPath(t *testing.T) {
	t.Parallel()

	if got := server.RouteClass("/accountable.system.v1.SystemService/GetRuntime"); got != "system" {
		t.Fatalf("registered route class = %s", got)
	}
	got := server.RouteClass("/private.v1.Secrets/Read?token=abc")
	if got != "unregistered" || strings.Contains(got, "token") {
		t.Fatalf("unregistered route class = %s", got)
	}
}

func TestOutcomeSeparatesCallerAndServerFailures(t *testing.T) {
	t.Parallel()

	if outcome, category := server.Outcome(nil); outcome != "success" || category != "none" {
		t.Fatalf("success = (%s, %s)", outcome, category)
	}

	callerError := apierror.New(context.Background(), apierror.InvalidInput, "errors.invalidInput", nil, 0)
	if outcome, category := server.Outcome(callerError); outcome != "success" || category != "invalid_input" {
		t.Fatalf("caller failure = (%s, %s)", outcome, category)
	}

	serverError := apierror.New(context.Background(), apierror.InternalFailure, "errors.internal", nil, 0)
	if outcome, category := server.Outcome(serverError); outcome != "failure" || category != "internal_failure" {
		t.Fatalf("server failure = (%s, %s)", outcome, category)
	}

	if outcome, category := server.Outcome(connect.NewError(connect.CodeUnknown, errors.New("no detail"))); outcome != "failure" ||
		category != "internal_failure" {
		t.Fatalf("detail-free failure = (%s, %s)", outcome, category)
	}
}
