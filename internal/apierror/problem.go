package apierror

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	errorv1 "github.com/acctbl/accountable/gen/go/accountable/error/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/durationpb"
)

type Category string

const (
	InvalidInput       Category = "invalid_input"
	Unauthenticated    Category = "unauthenticated"
	Forbidden          Category = "forbidden"
	NotFound           Category = "not_found"
	AlreadyExists      Category = "already_exists"
	FailedPrecondition Category = "failed_precondition"
	StaleVersion       Category = "stale_version"
	Conflict           Category = "conflict"
	QuotaExceeded      Category = "quota_exceeded"
	RateLimited        Category = "rate_limited"
	DeadlineExceeded   Category = "deadline_exceeded"
	OutOfRange         Category = "out_of_range"
	NotImplemented     Category = "not_implemented"
	Unavailable        Category = "unavailable"
	DataLoss           Category = "data_loss"
	InternalFailure    Category = "internal_failure"
)

type FieldViolation struct {
	FieldPath string
	Code      string
}

type identityKey struct{}

// Identity carries the public work identity of one request. The correlation ID
// is the journey identity; a trace ID is never public.
type Identity struct {
	RequestID     string
	CorrelationID string
}

func WithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, identity)
}

func IdentityFromContext(ctx context.Context) Identity {
	identity, _ := ctx.Value(identityKey{}).(Identity)
	return identity
}

func New(
	ctx context.Context,
	category Category,
	messageKey string,
	violations []FieldViolation,
	retryAfter time.Duration,
) *connect.Error {
	code := connectCode(category)
	identity := IdentityFromContext(ctx)
	detail := &errorv1.ProblemDetail{
		Code:       string(category),
		MessageKey: messageKey,
		RequestId:  identity.RequestID,
	}
	// A problem identity is never fabricated. When generation fails the field
	// stays empty and the request and correlation headers remain valid.
	if problemID, uuidErr := uuid.NewV7(); uuidErr == nil {
		detail.ProblemId = problemID.String()
	}
	for _, violation := range violations {
		detail.FieldViolations = append(detail.FieldViolations, &errorv1.FieldViolation{
			FieldPath: violation.FieldPath,
			Code:      violation.Code,
		})
	}
	if retryAfter > 0 {
		detail.RetryAfter = durationpb.New(retryAfter)
	}

	// The wire-level Connect message is deliberately generic. Client copy is
	// selected exclusively from the safe, allow-listed message_key detail.
	err := connect.NewError(code, errors.New("request failed"))
	if errorDetail, detailErr := connect.NewErrorDetail(detail); detailErr == nil {
		err.AddDetail(errorDetail)
	}
	return err
}

// CategoryOf reports the bounded problem category of one handler error. It reads
// the safe detail, never the error text.
func CategoryOf(err error) Category {
	if err == nil {
		return ""
	}
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		return InternalFailure
	}
	for _, detail := range connectErr.Details() {
		value, valueErr := detail.Value()
		if valueErr != nil {
			continue
		}
		problem, ok := value.(*errorv1.ProblemDetail)
		if !ok {
			continue
		}
		return Category(problem.GetCode())
	}
	return InternalFailure
}

func EnsureSafe(ctx context.Context, err error) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return New(ctx, DeadlineExceeded, "errors.deadlineExceeded", nil, 0)
	}

	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		for _, detail := range connectErr.Details() {
			value, valueErr := detail.Value()
			if valueErr == nil {
				if _, ok := value.(*errorv1.ProblemDetail); ok {
					return connectErr
				}
			}
		}
	}

	return New(ctx, InternalFailure, "errors.internal", nil, 0)
}

func connectCode(category Category) connect.Code {
	switch category {
	case InvalidInput:
		return connect.CodeInvalidArgument
	case Unauthenticated:
		return connect.CodeUnauthenticated
	case Forbidden:
		return connect.CodePermissionDenied
	case NotFound:
		return connect.CodeNotFound
	case AlreadyExists:
		return connect.CodeAlreadyExists
	case FailedPrecondition:
		return connect.CodeFailedPrecondition
	case StaleVersion, Conflict:
		return connect.CodeAborted
	case QuotaExceeded, RateLimited:
		return connect.CodeResourceExhausted
	case DeadlineExceeded:
		return connect.CodeDeadlineExceeded
	case OutOfRange:
		return connect.CodeOutOfRange
	case NotImplemented:
		return connect.CodeUnimplemented
	case Unavailable:
		return connect.CodeUnavailable
	case DataLoss:
		return connect.CodeDataLoss
	case InternalFailure:
		return connect.CodeInternal
	default:
		return connect.CodeInternal
	}
}
