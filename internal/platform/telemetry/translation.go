package telemetry

import (
	"encoding/json"
	"strings"
)

// AlarmNotification is the subset of the CloudWatch alarm notification schema that
// the translator trusts. Every other field is ignored on purpose.
type AlarmNotification struct {
	AlarmName        string `json:"AlarmName"`
	AlarmDescription string `json:"AlarmDescription"`
	AlarmArn         string `json:"AlarmArn"`
	AWSAccountID     string `json:"AWSAccountId"`
	Region           string `json:"Region"`
	NewStateValue    string `json:"NewStateValue"`
	StateChangeTime  string `json:"StateChangeTime"`
}

// AlertEnvelope is the version 1 output that email and IRM receive.
type AlertEnvelope struct {
	SchemaVersion    int    `json:"schema_version"`
	Status           string `json:"status"`
	DeduplicationKey string `json:"deduplication_key"`
	RecoveryKey      string `json:"recovery_key"`
	AlarmName        string `json:"alarm_name"`
	Owner            string `json:"owner"`
	Severity         string `json:"severity"`
	Action           string `json:"action"`
	Scope            string `json:"scope"`
	Signal           string `json:"signal"`
	StackName        string `json:"stack_name,omitempty"`
	Environment      string `json:"environment,omitempty"`
	CellID           string `json:"cell_id,omitempty"`
	Summary          string `json:"summary"`
	RunbookURL       string `json:"runbook_url"`
	RecoverySignal   string `json:"recovery_signal"`
	DashboardURL     string `json:"dashboard_url"`
	IdentityURL      string `json:"identity_url"`
	ChangedAt        string `json:"changed_at"`
}

// AlertTranslationPolicy is the trusted deployment identity of one environment
// translator. Every field must match the notification before translation.
type AlertTranslationPolicy struct {
	AccountID   string
	Region      string
	Environment string
	TopicARN    string
}

// TranslateAlarm converts one validated CloudWatch alarm notification into the
// alert envelope that the output topics carry.
func TranslateAlarm(raw []byte, receivedTopicARN string, policy AlertTranslationPolicy) (AlertEnvelope, error) {
	var notification AlarmNotification
	if err := decodeStrictAlarm(raw, &notification); err != nil {
		return AlertEnvelope{}, err
	}
	if receivedTopicARN == "" || receivedTopicARN != policy.TopicARN {
		return AlertEnvelope{}, contractError("alarm topic")
	}
	if notification.AWSAccountID != policy.AccountID || notification.Region != policy.Region {
		return AlertEnvelope{}, contractError("alarm account")
	}
	if !strings.HasPrefix(notification.AlarmArn, "arn:aws:cloudwatch:"+policy.Region+":"+policy.AccountID+":alarm:") ||
		!strings.HasSuffix(notification.AlarmArn, ":"+notification.AlarmName) ||
		!boundedAlarmName(notification.AlarmName) {
		return AlertEnvelope{}, contractError("alarm identity")
	}
	status, err := alertStatus(notification.NewStateValue)
	if err != nil {
		return AlertEnvelope{}, err
	}
	metadata, err := ParseAlertMetadata([]byte(notification.AlarmDescription))
	if err != nil {
		return AlertEnvelope{}, err
	}
	if metadata.Scope != "stack" && metadata.Environment != policy.Environment {
		return AlertEnvelope{}, contractError("alarm environment scope")
	}
	if !validWholeSecondUTC(notification.StateChangeTime) && !validMillisecondUTC(notification.StateChangeTime) {
		return AlertEnvelope{}, contractError("alarm state change time")
	}
	return AlertEnvelope{
		SchemaVersion:    ContractSchemaVersion,
		Status:           status,
		DeduplicationKey: notification.AlarmArn,
		RecoveryKey:      notification.AlarmArn,
		AlarmName:        notification.AlarmName,
		Owner:            metadata.Owner,
		Severity:         metadata.Severity,
		Action:           metadata.Action,
		Scope:            metadata.Scope,
		Signal:           metadata.Signal,
		StackName:        metadata.StackName,
		Environment:      metadata.Environment,
		CellID:           metadata.CellID,
		Summary:          metadata.Summary,
		RunbookURL:       metadata.RunbookURL,
		RecoverySignal:   metadata.RecoverySignal,
		DashboardURL:     metadata.DashboardURL,
		IdentityURL:      metadata.IdentityURL,
		ChangedAt:        notification.StateChangeTime,
	}, nil
}

// AlertOutputTopic reports which output topic carries one envelope.
func AlertOutputTopic(envelope AlertEnvelope) (string, error) {
	switch envelope.Severity {
	case "critical":
		return "critical", nil
	case "warning":
		return "warning", nil
	case "proof":
		return "proof", nil
	default:
		return "", contractError("alert severity")
	}
}

func alertStatus(state string) (string, error) {
	switch state {
	case "ALARM":
		return "firing", nil
	case "OK":
		return "resolved", nil
	default:
		return "", contractError("alarm state")
	}
}

func boundedAlarmName(name string) bool {
	return name != "" && len(name) <= 255 && boundedAlarmNamePattern.MatchString(name)
}

func decodeStrictAlarm(raw []byte, target *AlarmNotification) error {
	if len(raw) > 16<<10 {
		return contractError("alarm notification size")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return contractError("alarm notification")
	}
	return nil
}

func validMillisecondUTC(value string) bool {
	return millisecondUTCPattern.MatchString(value)
}
