package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/acctbl/accountable/internal/platform/secret"
	"github.com/acctbl/accountable/internal/platform/telemetry"
)

const (
	metadataTimeout = 5 * time.Second
	configFileName  = "collector.yaml"
	maxMetadataBody = 1 << 16
	maxDocumentSize = 8 << 10
)

var taskIdentityPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// document carries the non-secret Collector deployment settings that the cell
// module renders. Secret material is never part of it.
type document struct {
	SchemaVersion       int    `json:"schema_version"`
	Environment         string `json:"environment"`
	CellID              string `json:"cell_id"`
	AWSRegion           string `json:"aws_region"`
	ParameterPath       string `json:"parameter_path"`
	OTLPTokenRef        string `json:"otlp_token_ref"`
	GrafanaOTLPEndpoint string `json:"grafana_otlp_endpoint"`
	GrafanaInstanceID   string `json:"grafana_instance_id"`
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	configPath, metadataURI, err := parseArgs(args)
	if err != nil {
		return err
	}
	settings, err := readDocument(configPath)
	if err != nil {
		return err
	}
	taskID, err := taskIdentity(ctx, metadataURI)
	if err != nil {
		return err
	}
	token, err := resolveToken(ctx, settings)
	if err != nil {
		return err
	}
	rendered, err := telemetry.BuildCollectorConfig(telemetry.CollectorDeploymentConfig{
		SchemaVersion:           telemetry.ContractSchemaVersion,
		Environment:             settings.Environment,
		CellID:                  settings.CellID,
		CollectorTaskID:         taskID,
		AWSRegion:               settings.AWSRegion,
		GrafanaOTLPEndpoint:     settings.GrafanaOTLPEndpoint,
		GrafanaInstanceID:       settings.GrafanaInstanceID,
		GrafanaToken:            string(token),
		ApplicationEndpoint:     "0.0.0.0:" + strconv.Itoa(telemetry.CollectorApplicationPort),
		SLIEndpoint:             "0.0.0.0:" + strconv.Itoa(telemetry.CollectorSLIPort),
		InternalMetricsEndpoint: "127.0.0.1:" + strconv.Itoa(telemetry.CollectorInternalPort),
		Protocol:                telemetry.TelemetryProtocol,
		SamplingPolicy:          telemetry.TelemetrySamplingPolicy,
		RedactionPolicyVersion:  telemetry.TelemetryRedactionPolicy,
	})
	if err != nil {
		return err
	}
	writer, err := telemetry.NewScratchWriter(telemetry.ScratchDirectory)
	if err != nil {
		return err
	}
	return writer.Write(configFileName, []byte(rendered))
}

func parseArgs(args []string) (string, string, error) {
	usage := errors.New("usage: --config <absolute-path> --metadata-uri <url>")
	if len(args) != 4 || args[0] != "--config" || args[2] != "--metadata-uri" {
		return "", "", usage
	}
	configPath, metadataURI := args[1], args[3]
	if !filepath.IsAbs(configPath) {
		return "", "", usage
	}
	parsed, err := url.Parse(metadataURI)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" {
		return "", "", errors.New("--metadata-uri must be the ECS task metadata endpoint")
	}
	return filepath.Clean(configPath), metadataURI, nil
}

func readDocument(path string) (document, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return document{}, err
	}
	if len(raw) > maxDocumentSize {
		return document{}, errors.New("the Collector deployment document is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var settings document
	if err := decoder.Decode(&settings); err != nil {
		return document{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return document{}, errors.New("the Collector deployment document must be one JSON object")
	}
	if settings.SchemaVersion != telemetry.ContractSchemaVersion || settings.AWSRegion == "" ||
		settings.ParameterPath == "" || settings.OTLPTokenRef == "" || settings.GrafanaInstanceID == "" {
		return document{}, errors.New("the Collector deployment document is incomplete")
	}
	return settings, nil
}

func resolveToken(ctx context.Context, settings document) ([]byte, error) {
	ref, err := secret.ParseRef(settings.OTLPTokenRef)
	if err != nil {
		return nil, err
	}
	source, err := secret.NewSource(ctx, secret.Config{
		Provider:      secret.ProviderParameterStore,
		AWSRegion:     settings.AWSRegion,
		ParameterPath: settings.ParameterPath,
	})
	if err != nil {
		return nil, err
	}
	resolved, err := source.ResolveBatch(ctx, []secret.Ref{ref})
	if err != nil {
		return nil, err
	}
	value, ok := resolved[ref]
	if !ok {
		return nil, errors.New("the selected Grafana OTLP token is absent")
	}
	token := value.Bytes()
	if len(token) == 0 {
		return nil, errors.New("the selected Grafana OTLP token is empty")
	}
	return token, nil
}

func taskIdentity(ctx context.Context, metadataURI string) (string, error) {
	requestContext, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, metadataURI+"/task", nil)
	if err != nil {
		return "", err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("task metadata returned HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxMetadataBody))
	if err != nil {
		return "", err
	}
	var metadata struct {
		TaskARN string `json:"TaskARN"`
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return "", err
	}
	return taskIDFromARN(metadata.TaskARN)
}

func taskIDFromARN(arn string) (string, error) {
	separator := strings.LastIndex(arn, "/")
	if separator < 0 {
		return "", errors.New("task metadata has no task ARN")
	}
	identity := arn[separator+1:]
	if !taskIdentityPattern.MatchString(identity) {
		return "", errors.New("task ARN does not carry a 32-character hexadecimal task identity")
	}
	return identity, nil
}
