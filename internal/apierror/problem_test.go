package apierror_test

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"connectrpc.com/connect"
	errorv1 "github.com/acctbl/accountable/gen/go/accountable/error/v1"
	"github.com/acctbl/accountable/internal/apierror"
)

var uuidV7 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewAttachesSafeProblemDetail(t *testing.T) {
	ctx := apierror.WithIdentity(context.Background(), apierror.Identity{
		RequestID:     "01989c6a-bd3d-7b71-89f8-19f5c5f698ad",
		CorrelationID: "browser-trace",
	})
	err := apierror.New(ctx, apierror.Unavailable, "errors.unavailable", nil, 2*time.Second)

	if err.Code() != connect.CodeUnavailable {
		t.Fatalf("code = %v, want %v", err.Code(), connect.CodeUnavailable)
	}
	if err.Message() != "request failed" {
		t.Fatalf("unsafe wire message: %q", err.Message())
	}

	details := err.Details()
	if len(details) != 1 {
		t.Fatalf("details = %d, want 1", len(details))
	}
	value, valueErr := details[0].Value()
	if valueErr != nil {
		t.Fatalf("detail value: %v", valueErr)
	}
	problem, ok := value.(*errorv1.ProblemDetail)
	if !ok {
		t.Fatalf("detail type = %T, want *errorv1.ProblemDetail", value)
	}
	if problem.GetCode() != "unavailable" || problem.GetMessageKey() != "errors.unavailable" {
		t.Fatalf("unexpected detail: %v", problem)
	}
	if !uuidV7.MatchString(problem.GetProblemId()) {
		t.Fatalf("problem id %q is not UUIDv7", problem.GetProblemId())
	}
	if problem.GetRequestId() == "" || problem.GetRetryAfter().AsDuration() != 2*time.Second {
		t.Fatalf("missing correlation or retry detail: %v", problem)
	}
}

func TestCategoryConnectCodeMap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		category apierror.Category
		want     connect.Code
	}{
		{apierror.InvalidInput, connect.CodeInvalidArgument},
		{apierror.Unauthenticated, connect.CodeUnauthenticated},
		{apierror.Forbidden, connect.CodePermissionDenied},
		{apierror.NotFound, connect.CodeNotFound},
		{apierror.AlreadyExists, connect.CodeAlreadyExists},
		{apierror.FailedPrecondition, connect.CodeFailedPrecondition},
		{apierror.StaleVersion, connect.CodeAborted},
		{apierror.Conflict, connect.CodeAborted},
		{apierror.QuotaExceeded, connect.CodeResourceExhausted},
		{apierror.RateLimited, connect.CodeResourceExhausted},
		{apierror.DeadlineExceeded, connect.CodeDeadlineExceeded},
		{apierror.OutOfRange, connect.CodeOutOfRange},
		{apierror.NotImplemented, connect.CodeUnimplemented},
		{apierror.Unavailable, connect.CodeUnavailable},
		{apierror.DataLoss, connect.CodeDataLoss},
		{apierror.InternalFailure, connect.CodeInternal},
	}
	for _, test := range tests {
		t.Run(string(test.category), func(t *testing.T) {
			err := apierror.New(context.Background(), test.category, "errors.test", nil, 0)
			if err.Code() != test.want {
				t.Fatalf("code = %v, want %v", err.Code(), test.want)
			}
		})
	}
}

func TestFieldViolationUsesPathAndStableCode(t *testing.T) {
	t.Parallel()
	err := apierror.New(context.Background(), apierror.InvalidInput, "errors.invalidInput", []apierror.FieldViolation{{
		FieldPath: "command.lines[0].amount",
		Code:      "must_be_positive",
	}}, 0)
	value, valueErr := err.Details()[0].Value()
	if valueErr != nil {
		t.Fatalf("detail value: %v", valueErr)
	}
	problem := value.(*errorv1.ProblemDetail)
	violation := problem.GetFieldViolations()[0]
	if violation.GetFieldPath() != "command.lines[0].amount" || violation.GetCode() != "must_be_positive" {
		t.Fatalf("field violation = %v", violation)
	}
}

func TestEnsureSafeReplacesUnknownError(t *testing.T) {
	err := apierror.EnsureSafe(context.Background(), errors.New("sql: secret table path"))
	if err == nil || regexp.MustCompile(`sql|secret|path`).MatchString(err.Error()) {
		t.Fatalf("internal details escaped boundary: %v", err)
	}
}
