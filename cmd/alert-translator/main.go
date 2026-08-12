package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"

	"github.com/acctbl/accountable/internal/platform/telemetry"
)

// deployment carries the trusted identity of one environment translator. Lambda
// has no configuration file, so the deployment renders it as one JSON document.
type deployment struct {
	SchemaVersion int    `json:"schema_version"`
	AccountID     string `json:"account_id"`
	Region        string `json:"region"`
	Environment   string `json:"environment"`
	InputTopicARN string `json:"input_topic_arn"`
	CriticalTopic string `json:"critical_topic_arn"`
	WarningTopic  string `json:"warning_topic_arn"`
	ProofTopic    string `json:"proof_topic_arn"`
}

type publisher interface {
	Publish(context.Context, *sns.PublishInput, ...func(*sns.Options)) (*sns.PublishOutput, error)
}

type translator struct {
	deployment deployment
	publisher  publisher
}

func main() {
	handler, err := newTranslator(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	lambda.Start(handler.handle)
}

func newTranslator(ctx context.Context) (*translator, error) {
	raw, err := os.ReadFile("/var/task/deployment.json")
	if err != nil {
		return nil, err
	}
	var settings deployment
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, err
	}
	if settings.SchemaVersion != telemetry.ContractSchemaVersion || settings.AccountID == "" ||
		settings.Region == "" || settings.Environment == "" || settings.InputTopicARN == "" ||
		settings.CriticalTopic == "" || settings.WarningTopic == "" || settings.ProofTopic == "" {
		return nil, errors.New("the translator deployment document is incomplete")
	}
	awsConfig, err := config.LoadDefaultConfig(ctx, config.WithRegion(settings.Region))
	if err != nil {
		return nil, err
	}
	return &translator{deployment: settings, publisher: sns.NewFromConfig(awsConfig)}, nil
}

// handle translates every record or fails the whole event. A failed translation
// must never be silently dropped, because the direct failure route depends on the
// Lambda error metric.
func (t *translator) handle(ctx context.Context, event events.SNSEvent) error {
	for _, record := range event.Records {
		if err := t.translate(ctx, record); err != nil {
			return err
		}
	}
	return nil
}

func (t *translator) translate(ctx context.Context, record events.SNSEventRecord) error {
	policy := telemetry.AlertTranslationPolicy{
		AccountID:   t.deployment.AccountID,
		Region:      t.deployment.Region,
		Environment: t.deployment.Environment,
		TopicARN:    t.deployment.InputTopicARN,
	}
	envelope, err := telemetry.TranslateAlarm([]byte(record.SNS.Message), record.SNS.TopicArn, policy)
	if err != nil {
		return err
	}
	route, err := telemetry.AlertOutputTopic(envelope)
	if err != nil {
		return err
	}
	document, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	_, err = t.publisher.Publish(ctx, &sns.PublishInput{
		TopicArn: aws.String(t.outputTopic(route)),
		Subject:  aws.String(subject(envelope)),
		Message:  aws.String(string(document)),
	})
	return err
}

func (t *translator) outputTopic(route string) string {
	switch route {
	case "critical":
		return t.deployment.CriticalTopic
	case "warning":
		return t.deployment.WarningTopic
	default:
		return t.deployment.ProofTopic
	}
}

// subject carries only bounded values so no alert channel receives free text.
func subject(envelope telemetry.AlertEnvelope) string {
	return fmt.Sprintf("%s %s %s", envelope.Status, envelope.Severity, envelope.AlarmName)
}
