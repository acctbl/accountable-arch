package telemetry

type WorkIdentity struct {
	RequestID     string `json:"request_id,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
	OperationID   string `json:"operation_id,omitempty"`
	EventID       string `json:"event_id,omitempty"`
	ProblemID     string `json:"problem_id,omitempty"`
	CausationID   string `json:"causation_id,omitempty"`
}

func ParseWorkIdentity(raw []byte) (WorkIdentity, error) {
	var identity WorkIdentity
	if err := decodeStrictJSON(raw, &identity); err != nil {
		return WorkIdentity{}, err
	}
	if err := ValidateWorkIdentity(identity); err != nil {
		return WorkIdentity{}, err
	}
	return identity, nil
}

func ValidateWorkIdentity(identity WorkIdentity) error {
	values := []string{
		identity.RequestID,
		identity.CorrelationID,
		identity.OperationID,
		identity.EventID,
		identity.ProblemID,
		identity.CausationID,
	}
	for _, value := range values {
		if value != "" && !releaseIDPattern.MatchString(value) {
			return contractError("work identity")
		}
	}
	if identity.RequestID == "" || identity.CorrelationID == "" {
		return contractError("request identity")
	}
	return nil
}

func ValidateRootWorkIdentity(identity WorkIdentity) error {
	if err := ValidateWorkIdentity(identity); err != nil {
		return err
	}
	if identity.CorrelationID != identity.RequestID || identity.CausationID != "" {
		return contractError("root work identity")
	}
	return nil
}
