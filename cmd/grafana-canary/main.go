package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"

	"github.com/acctbl/accountable/internal/platform/telemetry"
)

// deployment is the trusted, non-secret identity of one canary. The Grafana
// token stays in Parameter Store and never appears here.
type deployment struct {
	SchemaVersion       int           `json:"schema_version"`
	Handler             string        `json:"handler"`
	Region              string        `json:"region"`
	Environment         string        `json:"environment"`
	StackName           string        `json:"stack_name"`
	GrafanaBaseURL      string        `json:"grafana_base_url"`
	TokenParameter      string        `json:"token_parameter"`
	Queries             []canaryQuery `json:"queries"`
	OrganizationSlug    string        `json:"organization_slug,omitempty"`
	StackTokenParameter string        `json:"stack_token_parameter,omitempty"`
	ApprovedPlan        string        `json:"approved_plan,omitempty"`
}

func main() {
	settings, err := readDeployment("/var/task/deployment.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx := context.Background()
	switch settings.Handler {
	case "canary":
		canary, err := newCanary(ctx, settings)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		lambda.Start(canary.handle)
	case "capacity":
		awsConfig, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(settings.Region))
		if err != nil {
			fmt.Fprintln(os.Stderr, "AWS configuration is unavailable")
			os.Exit(1)
		}
		producer, err := newCapacityProducer(ctx, settings, newCloudWatchCapacityStore(cloudwatch.NewFromConfig(awsConfig)))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		lambda.Start(producer.handle)
	default:
		fmt.Fprintln(os.Stderr, "the deployment handler is invalid")
		os.Exit(1)
	}
}

func readDeployment(path string) (deployment, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the deployment package owns this path
	if err != nil {
		return deployment{}, err
	}
	var settings deployment
	if err := json.Unmarshal(raw, &settings); err != nil {
		return deployment{}, err
	}
	if settings.SchemaVersion != telemetry.ContractSchemaVersion || settings.Region == "" || settings.Handler == "" ||
		settings.Environment == "" || settings.GrafanaBaseURL == "" || settings.TokenParameter == "" ||
		settings.StackName != telemetry.GrafanaStackSlug {
		return deployment{}, errors.New("the canary deployment document is incomplete")
	}
	if settings.Handler == "canary" && len(settings.Queries) == 0 {
		return deployment{}, errors.New("the canary deployment document lists no required query")
	}
	if settings.Handler == "capacity" && (settings.Environment != "development" || settings.OrganizationSlug == "" || settings.StackTokenParameter == "" || settings.ApprovedPlan == "") {
		return deployment{}, errors.New("the capacity deployment document is incomplete")
	}
	return settings, nil
}
