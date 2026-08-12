package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/acctbl/accountable/internal/platform/clock"
	"github.com/acctbl/accountable/internal/platform/telemetry"
)

const maxRecordSize = 4 << 10

type objectStore interface {
	ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

type settings struct {
	command     string
	bucket      string
	kmsKeyARN   string
	region      string
	environment string
	cellID      string
	state       string
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	parsed, err := parseArgs(args)
	if err != nil {
		return err
	}
	awsConfig, err := config.LoadDefaultConfig(ctx, config.WithRegion(parsed.region))
	if err != nil {
		return err
	}
	client := s3.NewFromConfig(awsConfig)
	switch parsed.command {
	case "read":
		records, err := readChain(ctx, client, parsed)
		if err != nil {
			return err
		}
		return emitChain(records)
	case "write":
		return writeRecord(ctx, client, parsed)
	default:
		return fmt.Errorf("unknown cell-inventory command %q", parsed.command)
	}
}

// readChain reads and validates the complete digest chain for one cell. An invalid,
// missing, conflicting, or incomplete chain is an error, never a partial result.
func readChain(ctx context.Context, client objectStore, parsed settings) ([]telemetry.InventoryRecord, error) {
	prefix := fmt.Sprintf("observability/v1/cell-inventory/%s/%s/", parsed.environment, parsed.cellID)
	var keys []string
	var token *string
	for {
		page, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(parsed.bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: token,
		})
		if err != nil {
			return nil, err
		}
		for _, object := range page.Contents {
			keys = append(keys, aws.ToString(object.Key))
		}
		if !aws.ToBool(page.IsTruncated) {
			break
		}
		token = page.NextContinuationToken
	}
	if len(keys) == 0 {
		return nil, nil
	}
	sort.Strings(keys)
	records := make([]telemetry.InventoryRecord, 0, len(keys))
	for _, key := range keys {
		raw, err := readObject(ctx, client, parsed.bucket, key)
		if err != nil {
			return nil, err
		}
		record, err := telemetry.ParseInventoryRecord(raw)
		if err != nil {
			return nil, fmt.Errorf("cell-inventory record %s: %w", key, err)
		}
		expected, err := telemetry.InventoryRecordKey(record)
		if err != nil {
			return nil, err
		}
		if expected != key {
			return nil, fmt.Errorf("cell-inventory record %s is not stored at its content address", key)
		}
		records = append(records, record)
	}
	if err := telemetry.ValidateInventoryChain(records); err != nil {
		return nil, err
	}
	return records, nil
}

func writeRecord(ctx context.Context, client objectStore, parsed settings) error {
	records, err := readChain(ctx, client, parsed)
	if err != nil {
		return err
	}
	deploymentID, err := telemetry.NewWorkID()
	if err != nil {
		return err
	}
	record := telemetry.InventoryRecord{
		SchemaVersion: telemetry.ContractSchemaVersion,
		Environment:   parsed.environment,
		CellID:        parsed.cellID,
		State:         parsed.state,
		EffectiveAt:   nextBoundary(clock.System{}.Now()),
		DeploymentID:  deploymentID,
	}
	if len(records) > 0 {
		previous := records[len(records)-1]
		digest, digestErr := telemetry.InventoryRecordDigest(previous)
		if digestErr != nil {
			return digestErr
		}
		record.PreviousRecordDigest = digest
	}
	if err := telemetry.ValidateInventoryChain(append(records, record)); err != nil {
		return err
	}
	document, err := json.Marshal(record)
	if err != nil {
		return err
	}
	canonical, err := telemetry.CanonicalizeJSON(document)
	if err != nil {
		return err
	}
	key, err := telemetry.InventoryRecordKey(record)
	if err != nil {
		return err
	}
	if _, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:               aws.String(parsed.bucket),
		Key:                  aws.String(key),
		Body:                 bytes.NewReader(canonical),
		ContentType:          aws.String("application/json"),
		ServerSideEncryption: types.ServerSideEncryptionAwsKms,
		SSEKMSKeyId:          aws.String(parsed.kmsKeyARN),
		IfNoneMatch:          aws.String("*"),
	}); err != nil {
		return err
	}
	return emitChain(append(records, record))
}

func readObject(ctx context.Context, client objectStore, bucket, key string) ([]byte, error) {
	object, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		return nil, err
	}
	defer func() { _ = object.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(object.Body, maxRecordSize+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxRecordSize {
		return nil, fmt.Errorf("cell-inventory record %s is too large", key)
	}
	return raw, nil
}

// nextBoundary reports the next complete 300-second boundary, which is the
// earliest time a state change can take effect.
func nextBoundary(now time.Time) string {
	return now.Truncate(300 * time.Second).Add(300 * time.Second).UTC().Format(time.RFC3339)
}

func emitChain(records []telemetry.InventoryRecord) error {
	encoded, err := json.Marshal(records)
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

func parseArgs(args []string) (settings, error) {
	usage := errors.New(
		"usage: cell-inventory <read|write> --bucket <name> --kms-key-arn <arn> --region <region> " +
			"--environment <name> --cell-id <id> [--state <active|inactive>]",
	)
	if len(args) < 1 {
		return settings{}, usage
	}
	parsed := settings{command: args[0]}
	rest := args[1:]
	if len(rest)%2 != 0 {
		return settings{}, usage
	}
	for index := 0; index < len(rest); index += 2 {
		value := rest[index+1]
		switch rest[index] {
		case "--bucket":
			parsed.bucket = value
		case "--kms-key-arn":
			parsed.kmsKeyARN = value
		case "--region":
			parsed.region = value
		case "--environment":
			parsed.environment = value
		case "--cell-id":
			parsed.cellID = value
		case "--state":
			parsed.state = value
		default:
			return settings{}, usage
		}
	}
	if parsed.bucket == "" || parsed.region == "" || parsed.environment == "" || parsed.cellID == "" {
		return settings{}, usage
	}
	if parsed.command == "write" && (parsed.kmsKeyARN == "" || parsed.state == "") {
		return settings{}, usage
	}
	return parsed, nil
}
