package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	errorv1 "github.com/acctbl/accountable/gen/go/accountable/error/v1"
	probev1 "github.com/acctbl/accountable/gen/go/accountable/probe/v1"
	"github.com/acctbl/accountable/gen/go/accountable/probe/v1/probev1connect"
	systemv1 "github.com/acctbl/accountable/gen/go/accountable/system/v1"
	"github.com/acctbl/accountable/gen/go/accountable/system/v1/systemv1connect"
	"github.com/acctbl/accountable/internal/bootstrap"
	"google.golang.org/protobuf/types/known/durationpb"
)

var integrationUUIDV7 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type stallingSystemServer struct {
	systemv1connect.UnimplementedSystemServiceHandler
}

func (stallingSystemServer) GetRuntime(
	ctx context.Context,
	_ *connect.Request[systemv1.GetRuntimeRequest],
) (*connect.Response[systemv1.GetRuntimeResponse], error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestProductionRouterBoundsStalledUnaryWork(t *testing.T) {
	t.Parallel()

	ready := &readiness{}
	ready.Set(true)
	handler := newAPIHandlerWithSystem(config{
		AllowedOrigins:    []string{"https://shell.example"},
		UnaryRPCDeadline:  25 * time.Millisecond,
		StreamRPCDeadline: time.Second,
	}, ready, testRelease(), nil, stallingSystemServer{})
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)

	client := systemv1connect.NewSystemServiceClient(httpServer.Client(), httpServer.URL)
	started := time.Now()
	_, err := client.GetRuntime(context.Background(), connect.NewRequest(&systemv1.GetRuntimeRequest{}))
	if connect.CodeOf(err) != connect.CodeDeadlineExceeded {
		t.Fatalf("GetRuntime code = %v, want deadline_exceeded", connect.CodeOf(err))
	}
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		t.Fatalf("GetRuntime error = %T, want *connect.Error", err)
	}
	if connectErr.Message() != "request failed" {
		t.Fatalf("unsafe deadline message = %q", connectErr.Message())
	}
	details := connectErr.Details()
	if len(details) != 1 {
		t.Fatalf("deadline details = %d, want 1", len(details))
	}
	value, valueErr := details[0].Value()
	if valueErr != nil {
		t.Fatalf("decode deadline detail: %v", valueErr)
	}
	problem, ok := value.(*errorv1.ProblemDetail)
	if !ok {
		t.Fatalf("deadline detail = %T, want *ProblemDetail", value)
	}
	if problem.GetCode() != "deadline_exceeded" || problem.GetMessageKey() != "errors.deadlineExceeded" {
		t.Fatalf("deadline detail = %v", problem)
	}
	if !integrationUUIDV7.MatchString(problem.GetProblemId()) || !integrationUUIDV7.MatchString(problem.GetRequestId()) {
		t.Fatalf("deadline detail lacks correlation IDs: %v", problem)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("stalled unary work escaped deadline: %v", elapsed)
	}
}

func TestProductionRouterIntegration(t *testing.T) {
	t.Parallel()

	ready := &readiness{}
	ready.Set(true)
	handler := newAPIHandler(config{
		ArchitectureProbe: true,
		AllowedOrigins:    []string{"https://shell.example"},
		UnaryRPCDeadline:  5 * time.Second,
		StreamRPCDeadline: 25 * time.Second,
	}, ready, testRelease(), nil)
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)

	systemClient := systemv1connect.NewSystemServiceClient(httpServer.Client(), httpServer.URL)
	systemRequest := connect.NewRequest(&systemv1.GetRuntimeRequest{})
	journey := "019261bb-4c10-7f3a-9aaa-0f1e2d3c4b5a"
	systemRequest.Header().Set("X-Correlation-ID", journey)
	runtime, err := systemClient.GetRuntime(context.Background(), systemRequest)
	if err != nil {
		t.Fatalf("GetRuntime through production router: %v", err)
	}
	if !integrationUUIDV7.MatchString(runtime.Msg.GetRequestId()) {
		t.Fatalf("request ID is not UUIDv7: %q", runtime.Msg.GetRequestId())
	}
	if runtime.Msg.GetRequestId() == "browser-journey-1" {
		t.Fatal("request and trace IDs must be distinct")
	}
	if runtime.Header().Get("X-Request-ID") != runtime.Msg.GetRequestId() ||
		runtime.Header().Get("X-Correlation-ID") != journey ||
		runtime.Header().Get("X-Request-ID") == journey {
		t.Fatalf("correlation headers do not match response: %v", runtime.Header())
	}

	probeClient := probev1connect.NewArchitectureProbeServiceClient(httpServer.Client(), httpServer.URL)
	failRequest := connect.NewRequest(&probev1.FailRequest{Kind: probev1.FailureKind_FAILURE_KIND_INVALID_INPUT})
	failRequest.Header().Set("X-Correlation-ID", "019261bb-4c10-7f3a-9aaa-0f1e2d3c4b5b")
	_, err = probeClient.Fail(context.Background(), failRequest)
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) || connectErr.Code() != connect.CodeInvalidArgument {
		t.Fatalf("typed validation failure: %v", err)
	}
	details := connectErr.Details()
	if len(details) != 1 {
		t.Fatalf("problem details = %d, want 1", len(details))
	}
	value, valueErr := details[0].Value()
	if valueErr != nil {
		t.Fatalf("decode detail: %v", valueErr)
	}
	problem, ok := value.(*errorv1.ProblemDetail)
	if !ok || problem.GetCode() != "invalid_input" || problem.GetMessageKey() != "errors.invalidInput" {
		t.Fatalf("unexpected safe detail: %T %v", value, value)
	}
	if !integrationUUIDV7.MatchString(problem.GetProblemId()) || !integrationUUIDV7.MatchString(problem.GetRequestId()) {
		t.Fatalf("missing UUIDv7 correlation: %v", problem)
	}
	violations := problem.GetFieldViolations()
	if len(violations) != 1 || violations[0].GetFieldPath() != "kind" || violations[0].GetCode() != "invalid_failure_kind" {
		t.Fatalf("unexpected field violations: %v", violations)
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancelRequest := connect.NewRequest(&probev1.WaitRequest{Delay: durationpb.New(10 * time.Second)})
	cancelled := make(chan error, 1)
	go func() {
		_, waitErr := probeClient.Wait(cancelCtx, cancelRequest)
		cancelled <- waitErr
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case waitErr := <-cancelled:
		if connect.CodeOf(waitErr) != connect.CodeCanceled {
			t.Fatalf("cancel code = %v, want canceled", connect.CodeOf(waitErr))
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not reach the probe boundary")
	}

	stream, err := probeClient.Stream(context.Background(), connect.NewRequest(&probev1.StreamRequest{
		Count:    3,
		Interval: durationpb.New(time.Millisecond),
	}))
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	var sequences []uint32
	for stream.Receive() {
		sequences = append(sequences, stream.Msg().GetSequence())
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("receive stream: %v", err)
	}
	if len(sequences) != 3 || sequences[0] != 1 || sequences[2] != 3 {
		t.Fatalf("stream sequences = %v", sequences)
	}
}

func TestArchitectureProbeGateAndRuntimeMiddlewareIntegration(t *testing.T) {
	t.Parallel()

	ready := &readiness{}
	ready.Set(true)
	disabledServer := httptest.NewServer(newAPIHandler(config{}, ready, testRelease(), nil))
	t.Cleanup(disabledServer.Close)
	response, err := disabledServer.Client().Post(
		disabledServer.URL+probev1connect.ArchitectureProbeServiceCorrelateProcedure,
		"application/json",
		strings.NewReader("{}"),
	)
	if err != nil {
		t.Fatalf("disabled probe request: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("disabled probe status = %d, want 404", response.StatusCode)
	}

	enabledServer := httptest.NewServer(newAPIHandler(config{
		ArchitectureProbe: true,
		AllowedOrigins:    []string{"http://localhost:3000"},
		UnaryRPCDeadline:  5 * time.Second,
		StreamRPCDeadline: 25 * time.Second,
		Foundation:        bootstrap.Config{Revision: "release-under-proof"},
	}, ready, testRelease(), nil))
	t.Cleanup(enabledServer.Close)
	preflight, err := http.NewRequest(http.MethodOptions,
		enabledServer.URL+probev1connect.ArchitectureProbeServiceCorrelateProcedure, nil)
	if err != nil {
		t.Fatalf("create preflight: %v", err)
	}
	preflight.Header.Set("Origin", "http://localhost:3000")
	preflight.Header.Set("Access-Control-Request-Method", "POST")
	preflightResponse, err := enabledServer.Client().Do(preflight)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	_ = preflightResponse.Body.Close()
	if preflightResponse.StatusCode != http.StatusNoContent ||
		preflightResponse.Header.Get("Access-Control-Allow-Credentials") != "true" ||
		preflightResponse.Header.Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatalf("credentialed CORS headers: %v", preflightResponse.Header)
	}

	probeClient := probev1connect.NewArchitectureProbeServiceClient(enabledServer.Client(), enabledServer.URL)
	cookieResponse, err := probeClient.CookieRoundTrip(context.Background(), connect.NewRequest(&probev1.CookieRoundTripRequest{SetCookie: true}))
	if err != nil {
		t.Fatalf("cookie probe: %v", err)
	}
	cookie := cookieResponse.Header().Get("Set-Cookie")
	for _, attribute := range []string{"Secure", "HttpOnly", "SameSite=Lax", "Max-Age=60"} {
		if !strings.Contains(cookie, attribute) {
			t.Fatalf("cookie %q missing %q", cookie, attribute)
		}
	}

	ready.Set(false)
	systemClient := systemv1connect.NewSystemServiceClient(enabledServer.Client(), enabledServer.URL)
	_, unavailableErr := systemClient.GetRuntime(context.Background(), connect.NewRequest(&systemv1.GetRuntimeRequest{}))
	var unavailableConnectErr *connect.Error
	if !errors.As(unavailableErr, &unavailableConnectErr) || unavailableConnectErr.Code() != connect.CodeUnavailable {
		t.Fatalf("not-ready RPC error = %v, want unavailable", unavailableErr)
	}
	if len(unavailableConnectErr.Details()) != 1 {
		t.Fatalf("not-ready RPC details = %d, want ProblemDetail", len(unavailableConnectErr.Details()))
	}
	unavailableValue, unavailableValueErr := unavailableConnectErr.Details()[0].Value()
	if unavailableValueErr != nil {
		t.Fatalf("decode not-ready ProblemDetail: %v", unavailableValueErr)
	}
	unavailableProblem, ok := unavailableValue.(*errorv1.ProblemDetail)
	if !ok || unavailableProblem.GetCode() != "unavailable" ||
		!integrationUUIDV7.MatchString(unavailableProblem.GetProblemId()) ||
		!integrationUUIDV7.MatchString(unavailableProblem.GetRequestId()) {
		t.Fatalf("not-ready detail = %T %v, want correlated unavailable ProblemDetail", unavailableValue, unavailableValue)
	}
	readyResponse, err := enabledServer.Client().Get(enabledServer.URL + "/_health/ready")
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	_ = readyResponse.Body.Close()
	if readyResponse.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("readiness status = %d", readyResponse.StatusCode)
	}
	liveResponse, err := enabledServer.Client().Get(enabledServer.URL + "/_health/live")
	if err != nil {
		t.Fatalf("liveness: %v", err)
	}
	if liveResponse.StatusCode != http.StatusOK {
		t.Fatalf("liveness status = %d", liveResponse.StatusCode)
	}
	var liveBody struct {
		Status                     string `json:"status"`
		ReleaseID                  string `json:"release_id"`
		ServiceVersion             string `json:"service_version"`
		ConfigurationSchemaVersion int    `json:"configuration_schema_version"`
		ConfigurationRevision      string `json:"configuration_revision"`
		ConfigurationFingerprint   string `json:"configuration_fingerprint"`
		RequestID                  string `json:"request_id"`
		CorrelationID              string `json:"correlation_id"`
	}
	if err := json.NewDecoder(liveResponse.Body).Decode(&liveBody); err != nil {
		t.Fatalf("decode liveness body: %v", err)
	}
	_ = liveResponse.Body.Close()
	release := testRelease()
	// Release, artifact, and configuration identity stay separate, and a health
	// call starts one root journey, so correlation equals request.
	if liveBody.Status != "ok" || liveBody.ReleaseID != release.ReleaseID ||
		liveBody.ServiceVersion != release.ServiceVersion ||
		liveBody.ConfigurationRevision != "release-under-proof" ||
		liveBody.ReleaseID == liveBody.ConfigurationRevision ||
		!integrationUUIDV7.MatchString(liveBody.RequestID) ||
		liveBody.CorrelationID != liveBody.RequestID ||
		liveResponse.Header.Get("X-Request-ID") != liveBody.RequestID ||
		liveResponse.Header.Get("X-Correlation-ID") != liveBody.RequestID {
		t.Fatalf("liveness body or headers = (%+v, %v)", liveBody, liveResponse.Header)
	}
	for _, oldPath := range []string{"/live", "/ready"} {
		response, requestErr := enabledServer.Client().Get(enabledServer.URL + oldPath)
		if requestErr != nil {
			t.Fatalf("old health path %s: %v", oldPath, requestErr)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("old health path %s status = %d, want 404", oldPath, response.StatusCode)
		}
	}
}
