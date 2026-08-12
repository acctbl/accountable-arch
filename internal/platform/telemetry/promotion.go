package telemetry

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

var sharedPromotionGateNames = []string{
	"budget",
	"capacity",
	"critical_alarms",
	"infrastructure",
	"loss",
	"paging",
	"privacy",
	"proof",
}

type PromotionGate struct {
	Name        string    `json:"name"`
	State       GateState `json:"state"`
	ObservedAt  string    `json:"observed_at"`
	WindowStart string    `json:"window_start,omitempty"`
	WindowEnd   string    `json:"window_end,omitempty"`
}

type PromotionEvidence struct {
	SchemaVersion         int               `json:"schema_version"`
	EvidenceID            string            `json:"evidence_id"`
	SourceEnvironment     string            `json:"source_environment"`
	TargetEnvironment     string            `json:"target_environment"`
	EvaluatedAt           string            `json:"evaluated_at"`
	WorkflowRunID         string            `json:"workflow_run_id"`
	WorkflowIdentity      string            `json:"workflow_identity"`
	ReleaseID             string            `json:"release_id"`
	ReleaseManifestDigest string            `json:"release_manifest_digest"`
	SignatureBundleDigest string            `json:"signature_bundle_digest"`
	Artifacts             []ReleaseArtifact `json:"artifacts"`
	Gates                 []PromotionGate   `json:"gates"`
	Decision              GateState         `json:"decision"`
}

func NewPromotionEvidence(evidence PromotionEvidence) (PromotionEvidence, error) {
	evidence.SchemaVersion = ContractSchemaVersion
	slices.SortFunc(evidence.Gates, func(left, right PromotionGate) int { return compareStrings(left.Name, right.Name) })
	slices.SortFunc(evidence.Artifacts, func(left, right ReleaseArtifact) int { return compareStrings(left.Name, right.Name) })
	evidence.Decision = promotionDecision(evidence.Gates)
	if err := ValidatePromotionEvidence(evidence); err != nil {
		return PromotionEvidence{}, err
	}
	return evidence, nil
}

func ParsePromotionEvidence(raw []byte) (PromotionEvidence, error) {
	var evidence PromotionEvidence
	if err := decodeStrictJSON(raw, &evidence); err != nil {
		return PromotionEvidence{}, err
	}
	if err := ValidatePromotionEvidence(evidence); err != nil {
		return PromotionEvidence{}, err
	}
	return evidence, nil
}

func ValidatePromotionEvidence(evidence PromotionEvidence) error {
	if evidence.SchemaVersion != ContractSchemaVersion || !uuidV7Pattern.MatchString(evidence.EvidenceID) ||
		!contains(registeredEnvironments, evidence.SourceEnvironment) || !contains(registeredEnvironments, evidence.TargetEnvironment) ||
		evidence.SourceEnvironment == evidence.TargetEnvironment || !uuidV7Pattern.MatchString(evidence.ReleaseID) ||
		!digestPattern.MatchString(evidence.ReleaseManifestDigest) || !digestPattern.MatchString(evidence.SignatureBundleDigest) ||
		evidence.WorkflowRunID == "" || !validPromotionWorkflowIdentity(evidence.WorkflowIdentity) || !canonicalSecond(evidence.EvaluatedAt) {
		return contractError("promotion evidence")
	}
	if (evidence.SourceEnvironment != "development" || evidence.TargetEnvironment != "staging") &&
		(evidence.SourceEnvironment != "staging" || evidence.TargetEnvironment != "production") {
		return contractError("promotion path")
	}
	if len(evidence.Artifacts) != len(releaseArtifactTypes) {
		return contractError("promotion artifacts")
	}
	for index, artifact := range evidence.Artifacts {
		if artifact.Name != []string{"api_oci", "collector_oci", "web_assets"}[index] {
			return contractError("promotion artifacts")
		}
	}
	if err := ValidateReleaseManifest(ReleaseManifest{
		SchemaVersion: ContractSchemaVersion,
		ReleaseID:     evidence.ReleaseID,
		CreatedAt:     evidence.EvaluatedAt,
		CommitSHA:     "0000000000000000000000000000000000000000",
		Artifacts:     evidence.Artifacts,
	}); err != nil {
		return contractError("promotion artifacts")
	}
	expectedGates := promotionGateNames(evidence.TargetEnvironment)
	if len(evidence.Gates) != len(expectedGates) || promotionDecision(evidence.Gates) != evidence.Decision {
		return contractError("promotion gates")
	}
	evaluatedAt, _ := time.Parse(time.RFC3339, evidence.EvaluatedAt)
	for index, gate := range evidence.Gates {
		if gate.Name != expectedGates[index] || !validGateState(gate.State) ||
			!canonicalSecond(gate.ObservedAt) || !canonicalSecond(gate.WindowStart) || !canonicalSecond(gate.WindowEnd) {
			return contractError("promotion gate")
		}
		observedAt, _ := time.Parse(time.RFC3339, gate.ObservedAt)
		windowStart, _ := time.Parse(time.RFC3339, gate.WindowStart)
		windowEnd, _ := time.Parse(time.RFC3339, gate.WindowEnd)
		maximumAge := 30 * time.Minute
		maximumWindowLag := 15 * time.Minute
		if gate.Name == "staging_observation" {
			maximumAge = 24*time.Hour + 15*time.Minute
			maximumWindowLag = maximumAge
		}
		if !windowStart.Before(windowEnd) || windowEnd.After(evaluatedAt) || evaluatedAt.Sub(windowEnd) > maximumWindowLag {
			return contractError("promotion gate window")
		}
		if gate.State != GateUnknown && (observedAt.Before(windowStart) || observedAt.After(windowEnd) || evaluatedAt.Sub(observedAt) > maximumAge) {
			return contractError("promotion gate freshness")
		}
		if gate.Name == "budget" && windowEnd.Sub(windowStart) != 30*24*time.Hour {
			return contractError("promotion budget window")
		}
		if gate.Name == "loss" && windowEnd.Sub(windowStart) != 7*24*time.Hour {
			return contractError("promotion loss window")
		}
		if gate.Name == "staging_observation" && windowEnd.Sub(windowStart) != stagingWindow {
			return contractError("promotion staging observation window")
		}
	}
	return nil
}

func promotionGateNames(targetEnvironment string) []string {
	names := slices.Clone(sharedPromotionGateNames)
	if targetEnvironment == "production" {
		names = append(names, "staging_observation")
		slices.Sort(names)
	}
	return names
}

func validPromotionWorkflowIdentity(value string) bool {
	const prefix = "https://github.com/"
	const suffix = "/.github/workflows/promotion.yml@refs/heads/main"
	return len(value) > len(prefix)+len(suffix) && value[:len(prefix)] == prefix && value[len(value)-len(suffix):] == suffix
}

func validGateState(state GateState) bool {
	return state == GatePass || state == GateFail || state == GateUnknown
}

func PromotionEvidenceBytes(evidence PromotionEvidence) ([]byte, error) {
	if err := ValidatePromotionEvidence(evidence); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		return nil, err
	}
	return CanonicalizeJSON(raw)
}

func PromotionEvidenceKey(evidence PromotionEvidence) (string, error) {
	raw, err := PromotionEvidenceBytes(evidence)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("observability/v1/promotion-evidence/%s/%s/%s-%s.json", evidence.TargetEnvironment, evidence.ReleaseID, evidence.EvaluatedAt, "sha256:"+sha256Hex(raw)), nil
}

func promotionDecision(gates []PromotionGate) GateState {
	decision := GatePass
	for _, gate := range gates {
		if gate.State == GateUnknown {
			return GateUnknown
		}
		if gate.State == GateFail {
			decision = GateFail
		}
	}
	return decision
}

func canonicalSecond(value string) bool {
	parsed, err := time.Parse(time.RFC3339, value)
	return err == nil && parsed.Location() == time.UTC && parsed.Nanosecond() == 0 && parsed.Format(time.RFC3339) == value
}

func compareStrings(left, right string) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
