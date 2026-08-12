package telemetry

import (
	"context"

	"github.com/google/uuid"
)

const (
	HeaderRequestID     = "X-Request-ID"
	HeaderCorrelationID = "X-Correlation-ID"
)

type workIdentityKey struct{}

// NewWorkID makes one UUIDv7 work identity. A generation failure never yields a
// substitute identity from another field.
func NewWorkID() (string, error) {
	value, err := uuid.NewV7()
	if err != nil {
		return "", contractError("identity generation")
	}
	return value.String(), nil
}

// StartRequestIdentity starts a new root journey when the inbound correlation ID
// is absent or invalid, which the equal request and correlation IDs record.
func StartRequestIdentity(inboundCorrelationID string) (WorkIdentity, error) {
	requestID, err := NewWorkID()
	if err != nil {
		return WorkIdentity{}, err
	}
	correlationID := requestID
	if uuidV7Pattern.MatchString(inboundCorrelationID) {
		correlationID = inboundCorrelationID
	}
	return WorkIdentity{RequestID: requestID, CorrelationID: correlationID}, nil
}

// WithOperation makes the operation identity for one server unit of work and
// copies the causal identity only from trusted context.
func WithOperation(identity WorkIdentity, causationID string) (WorkIdentity, error) {
	operationID, err := NewWorkID()
	if err != nil {
		return WorkIdentity{}, err
	}
	identity.OperationID = operationID
	if causationID != "" {
		if !uuidV7Pattern.MatchString(causationID) {
			return WorkIdentity{}, contractError("causation identity")
		}
		identity.CausationID = causationID
	}
	return identity, nil
}

func ContextWithWorkIdentity(ctx context.Context, identity WorkIdentity) context.Context {
	return context.WithValue(ctx, workIdentityKey{}, identity)
}

func WorkIdentityFromContext(ctx context.Context) (WorkIdentity, bool) {
	identity, ok := ctx.Value(workIdentityKey{}).(WorkIdentity)
	return identity, ok
}

// WorkIdentityAttributes reports the identity fields that logs and spans may
// carry. Metrics never receive them.
func WorkIdentityAttributes(identity WorkIdentity) map[string]string {
	attributes := make(map[string]string, 6)
	for name, value := range map[string]string{
		"request_id":     identity.RequestID,
		"correlation_id": identity.CorrelationID,
		"operation_id":   identity.OperationID,
		"event_id":       identity.EventID,
		"problem_id":     identity.ProblemID,
		"causation_id":   identity.CausationID,
	} {
		if value != "" {
			attributes[name] = value
		}
	}
	return attributes
}
