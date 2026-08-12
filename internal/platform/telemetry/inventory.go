package telemetry

import (
	"encoding/json"
	"fmt"
	"time"
)

type InventoryRecord struct {
	SchemaVersion        int    `json:"schema_version"`
	Environment          string `json:"environment"`
	CellID               string `json:"cell_id"`
	State                string `json:"state"`
	EffectiveAt          string `json:"effective_at"`
	DeploymentID         string `json:"deployment_id"`
	PreviousRecordDigest string `json:"previous_record_digest,omitempty"`
}

func ParseInventoryRecord(raw []byte) (InventoryRecord, error) {
	var record InventoryRecord
	if err := decodeStrictJSON(raw, &record); err != nil {
		return InventoryRecord{}, err
	}
	if err := ValidateInventoryRecord(record); err != nil {
		return InventoryRecord{}, err
	}
	return record, nil
}

func ValidateInventoryRecord(record InventoryRecord) error {
	if record.SchemaVersion != ContractSchemaVersion || !contains(registeredEnvironments, record.Environment) ||
		!cellIDPattern.MatchString(record.CellID) || !contains([]string{"active", "inactive"}, record.State) ||
		!uuidV7Pattern.MatchString(record.DeploymentID) {
		return contractError("inventory record")
	}
	effectiveAt, err := time.Parse(time.RFC3339, record.EffectiveAt)
	if err != nil || effectiveAt.Location() != time.UTC || effectiveAt.Nanosecond() != 0 ||
		effectiveAt.Format(time.RFC3339) != record.EffectiveAt || effectiveAt.Unix()%300 != 0 {
		return contractError("inventory effective_at")
	}
	if record.PreviousRecordDigest != "" && !digestPattern.MatchString(record.PreviousRecordDigest) {
		return contractError("previous_record_digest")
	}
	return nil
}

func InventoryRecordDigest(record InventoryRecord) (string, error) {
	if err := ValidateInventoryRecord(record); err != nil {
		return "", err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	canonical, err := CanonicalizeJSON(raw)
	if err != nil {
		return "", err
	}
	return "sha256:" + sha256Hex(canonical), nil
}

func ValidateInventoryChain(records []InventoryRecord) error {
	if len(records) == 0 {
		return contractError("inventory chain")
	}
	if records[0].State != "active" || records[0].PreviousRecordDigest != "" {
		return contractError("first inventory record")
	}
	deploymentIDs := make(map[string]struct{}, len(records))
	var previousDigest string
	var previousTime time.Time
	for index, record := range records {
		if err := ValidateInventoryRecord(record); err != nil {
			return err
		}
		if _, duplicate := deploymentIDs[record.DeploymentID]; duplicate {
			return contractError("inventory deployment_id")
		}
		deploymentIDs[record.DeploymentID] = struct{}{}
		effectiveAt, _ := time.Parse(time.RFC3339, record.EffectiveAt)
		if index > 0 {
			if record.Environment != records[0].Environment || record.CellID != records[0].CellID ||
				record.State == records[index-1].State || !effectiveAt.After(previousTime) ||
				record.PreviousRecordDigest != previousDigest {
				return contractError("inventory chain")
			}
		}
		digest, err := InventoryRecordDigest(record)
		if err != nil {
			return err
		}
		previousDigest = digest
		previousTime = effectiveAt
	}
	return nil
}

func InventoryRecordKey(record InventoryRecord) (string, error) {
	digest, err := InventoryRecordDigest(record)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"observability/v1/cell-inventory/%s/%s/%s-%s.json",
		record.Environment,
		record.CellID,
		record.EffectiveAt,
		digest,
	), nil
}
