package telemetry

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func proofPolicy() AlertTranslationPolicy {
	return AlertTranslationPolicy{
		AccountID:   "453722413624",
		Region:      "eu-west-2",
		Environment: "development",
		TopicARN:    "arn:aws:sns:eu-west-2:453722413624:accountable-development-observability-alarm-input",
	}
}

func alarmNotification(t *testing.T, mutate func(map[string]any)) ([]byte, string) {
	t.Helper()
	description, err := json.Marshal(AlertMetadata{
		SchemaVersion:  ContractSchemaVersion,
		Owner:          "platform",
		Severity:       "critical",
		Action:         "page",
		Scope:          "environment",
		Signal:         "grafana.canary",
		Environment:    "development",
		Summary:        "Grafana is unreachable",
		RunbookURL:     "https://docs.example/runbooks/grafana-canary",
		RecoverySignal: "two consecutive successful runs",
		DashboardURL:   "https://dashboards.example/service",
		IdentityURL:    "https://dashboards.example/identity",
	})
	if err != nil {
		t.Fatal(err)
	}
	notification := map[string]any{
		"AlarmName":        "accountable-development-observability-canary",
		"AlarmDescription": string(description),
		"AlarmArn": "arn:aws:cloudwatch:eu-west-2:453722413624:alarm:" +
			"accountable-development-observability-canary",
		"AWSAccountId":    "453722413624",
		"Region":          "eu-west-2",
		"NewStateValue":   "ALARM",
		"StateChangeTime": "2026-08-11T10:00:00Z",
	}
	if mutate != nil {
		mutate(notification)
	}
	raw, err := json.Marshal(notification)
	if err != nil {
		t.Fatal(err)
	}
	return raw, proofPolicy().TopicARN
}

func TestTranslateAlarmAcceptsATrustedNotification(t *testing.T) {
	t.Parallel()

	raw, topic := alarmNotification(t, nil)
	envelope, err := TranslateAlarm(raw, topic, proofPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Status != "firing" {
		t.Fatalf("status = %s", envelope.Status)
	}
	// One alarm is one incident, so deduplication and recovery share the key.
	if envelope.DeduplicationKey != envelope.RecoveryKey || !strings.HasPrefix(envelope.DeduplicationKey, "arn:aws:cloudwatch:") {
		t.Fatalf("keys = (%s, %s)", envelope.DeduplicationKey, envelope.RecoveryKey)
	}
	route, err := AlertOutputTopic(envelope)
	if err != nil || route != "critical" {
		t.Fatalf("route = (%s, %v)", route, err)
	}
}

func TestTranslateAlarmMapsRecovery(t *testing.T) {
	t.Parallel()

	raw, topic := alarmNotification(t, func(notification map[string]any) {
		notification["NewStateValue"] = "OK"
	})
	envelope, err := TranslateAlarm(raw, topic, proofPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Status != "resolved" {
		t.Fatalf("status = %s", envelope.Status)
	}
}

func TestTranslateAlarmRejectsUntrustedNotifications(t *testing.T) {
	t.Parallel()

	rejected := map[string]func(map[string]any){
		"another account": func(notification map[string]any) {
			notification["AWSAccountId"] = "000000000000"
		},
		"another region": func(notification map[string]any) {
			notification["Region"] = "us-east-1"
		},
		"alarm ARN from another account": func(notification map[string]any) {
			notification["AlarmArn"] = "arn:aws:cloudwatch:eu-west-2:000000000000:alarm:accountable-development-observability-canary"
		},
		"alarm ARN naming another alarm": func(notification map[string]any) {
			notification["AlarmArn"] = "arn:aws:cloudwatch:eu-west-2:453722413624:alarm:someone-elses-alarm"
		},
		"unknown state": func(notification map[string]any) {
			notification["NewStateValue"] = "INSUFFICIENT_DATA"
		},
		"another environment": func(notification map[string]any) {
			notification["AlarmDescription"] = strings.Replace(
				notification["AlarmDescription"].(string), `"environment":"development"`, `"environment":"production"`, 1)
		},
		"unregistered severity": func(notification map[string]any) {
			notification["AlarmDescription"] = strings.Replace(
				notification["AlarmDescription"].(string), `"severity":"critical"`, `"severity":"urgent"`, 1)
		},
		"severity and action disagree": func(notification map[string]any) {
			notification["AlarmDescription"] = strings.Replace(
				notification["AlarmDescription"].(string), `"action":"page"`, `"action":"warning"`, 1)
		},
		"description is not metadata": func(notification map[string]any) {
			notification["AlarmDescription"] = "the canary failed"
		},
		"description is oversized": func(notification map[string]any) {
			notification["AlarmDescription"] = strings.Repeat("a", 1025)
		},
	}
	for name, mutate := range rejected {
		t.Run(name, func(t *testing.T) {
			raw, topic := alarmNotification(t, mutate)
			if _, err := TranslateAlarm(raw, topic, proofPolicy()); !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestTranslateAlarmRejectsAnotherTopic(t *testing.T) {
	t.Parallel()

	raw, _ := alarmNotification(t, nil)
	elsewhere := "arn:aws:sns:eu-west-2:453722413624:someone-elses-topic"
	if _, err := TranslateAlarm(raw, elsewhere, proofPolicy()); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("got %v", err)
	}
	if _, err := TranslateAlarm(raw, "", proofPolicy()); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("empty topic returned %v", err)
	}
}

// A stack alert has no environment, so it must translate on any environment
// translator without claiming one.
func TestTranslateAlarmAcceptsAStackScopedAlert(t *testing.T) {
	t.Parallel()

	raw, topic := alarmNotification(t, func(notification map[string]any) {
		description := notification["AlarmDescription"].(string)
		description = strings.Replace(description, `"scope":"environment"`, `"scope":"stack"`, 1)
		description = strings.Replace(description, `"environment":"development",`, `"stack_name":"`+GrafanaStackSlug+`",`, 1)
		notification["AlarmDescription"] = description
	})
	envelope, err := TranslateAlarm(raw, topic, proofPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Scope != "stack" || envelope.StackName != GrafanaStackSlug || envelope.Environment != "" {
		t.Fatalf("envelope = %+v", envelope)
	}
}
