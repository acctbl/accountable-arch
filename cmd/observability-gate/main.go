package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/acctbl/accountable/internal/platform/telemetry"
)

const maximumGateInput = 8 << 20

type promotionRequest struct {
	SourceEnvironment     string                      `json:"source_environment"`
	TargetEnvironment     string                      `json:"target_environment"`
	EvaluatedAt           string                      `json:"evaluated_at"`
	WorkflowRunID         string                      `json:"workflow_run_id"`
	WorkflowIdentity      string                      `json:"workflow_identity"`
	ReleaseID             string                      `json:"release_id"`
	ReleaseManifestDigest string                      `json:"release_manifest_digest"`
	SignatureBundleDigest string                      `json:"signature_bundle_digest"`
	Artifacts             []telemetry.ReleaseArtifact `json:"artifacts"`
	Gates                 []telemetry.PromotionGate   `json:"gates"`
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, input io.Reader, output io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: observability-gate <budget|budget-snapshot|staging-observation-snapshot|loss|promotion|promotion-key>")
	}
	raw, err := io.ReadAll(io.LimitReader(input, maximumGateInput+1))
	if err != nil || len(raw) == 0 || len(raw) > maximumGateInput {
		return errors.New("the gate input is invalid")
	}
	switch args[0] {
	case "budget":
		var value telemetry.BudgetInput
		if err := decode(raw, &value); err != nil {
			return err
		}
		return encode(output, telemetry.EvaluateBudget(value))
	case "budget-snapshot":
		var value telemetry.BudgetSnapshot
		if err := decode(raw, &value); err != nil {
			return err
		}
		return encode(output, telemetry.EvaluateBudgetSnapshot(value))
	case "staging-observation-snapshot":
		var value telemetry.BudgetSnapshot
		if err := decode(raw, &value); err != nil {
			return err
		}
		return encode(output, telemetry.EvaluateStagingObservationSnapshot(value))
	case "loss":
		var value telemetry.LossGateInput
		if err := decode(raw, &value); err != nil {
			return err
		}
		return encode(output, map[string]telemetry.GateState{"state": telemetry.EvaluateLossGate(value)})
	case "promotion":
		var request promotionRequest
		if err := decode(raw, &request); err != nil {
			return err
		}
		evidenceID, err := telemetry.NewWorkID()
		if err != nil {
			return err
		}
		evidence, err := telemetry.NewPromotionEvidence(telemetry.PromotionEvidence{
			EvidenceID: evidenceID, SourceEnvironment: request.SourceEnvironment,
			TargetEnvironment: request.TargetEnvironment, EvaluatedAt: request.EvaluatedAt,
			WorkflowRunID: request.WorkflowRunID, WorkflowIdentity: request.WorkflowIdentity,
			ReleaseID: request.ReleaseID, ReleaseManifestDigest: request.ReleaseManifestDigest,
			SignatureBundleDigest: request.SignatureBundleDigest, Artifacts: request.Artifacts, Gates: request.Gates,
		})
		if err != nil {
			return err
		}
		canonical, err := telemetry.PromotionEvidenceBytes(evidence)
		if err != nil {
			return err
		}
		_, err = output.Write(append(canonical, '\n'))
		return err
	case "promotion-key":
		evidence, err := telemetry.ParsePromotionEvidence(raw)
		if err != nil {
			return err
		}
		key, err := telemetry.PromotionEvidenceKey(evidence)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, key)
		return err
	default:
		return errors.New("usage: observability-gate <budget|budget-snapshot|staging-observation-snapshot|loss|promotion|promotion-key>")
	}
}

func decode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("the gate input contains trailing data")
	}
	return nil
}

func encode(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}
