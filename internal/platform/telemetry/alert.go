package telemetry

import "unicode/utf8"

var alertActions = map[string]string{
	"critical": "page",
	"warning":  "warning",
	"proof":    "proof",
}

type AlertMetadata struct {
	SchemaVersion  int    `json:"schema_version"`
	Owner          string `json:"owner"`
	Severity       string `json:"severity"`
	Action         string `json:"action"`
	Scope          string `json:"scope"`
	Signal         string `json:"signal"`
	StackName      string `json:"stack_name,omitempty"`
	Environment    string `json:"environment,omitempty"`
	CellID         string `json:"cell_id,omitempty"`
	Summary        string `json:"summary"`
	RunbookURL     string `json:"runbook_url"`
	RecoverySignal string `json:"recovery_signal"`
	DashboardURL   string `json:"dashboard_url"`
	IdentityURL    string `json:"identity_url"`
}

func ParseAlertMetadata(raw []byte) (AlertMetadata, error) {
	if len(raw) > 1024 || !utf8.Valid(raw) {
		return AlertMetadata{}, contractError("AlarmDescription")
	}
	var metadata AlertMetadata
	if err := decodeStrictJSON(raw, &metadata); err != nil {
		return AlertMetadata{}, err
	}
	if err := ValidateAlertMetadata(metadata); err != nil {
		return AlertMetadata{}, err
	}
	return metadata, nil
}

func ValidateAlertMetadata(metadata AlertMetadata) error {
	if metadata.SchemaVersion != ContractSchemaVersion || metadata.Owner != "platform" {
		return contractError("alert owner")
	}
	action, ok := alertActions[metadata.Severity]
	if !ok || action != metadata.Action || !boundedNamePattern.MatchString(metadata.Signal) {
		return contractError("alert labels")
	}
	if metadata.Summary == "" || metadata.RunbookURL == "" || metadata.RecoverySignal == "" || metadata.DashboardURL == "" || metadata.IdentityURL == "" {
		return contractError("alert annotations")
	}
	switch metadata.Scope {
	case "stack":
		if metadata.StackName != "accountableshared" || metadata.Environment != "" || metadata.CellID != "" {
			return contractError("stack alert scope")
		}
	case "environment":
		if !contains(registeredEnvironments, metadata.Environment) || metadata.StackName != "" || metadata.CellID != "" {
			return contractError("environment alert scope")
		}
	case "cell":
		if !contains(registeredEnvironments, metadata.Environment) || !cellIDPattern.MatchString(metadata.CellID) || metadata.StackName != "" {
			return contractError("cell alert scope")
		}
	default:
		return contractError("alert scope")
	}
	return nil
}
