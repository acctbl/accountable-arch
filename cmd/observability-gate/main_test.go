package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/acctbl/accountable/internal/platform/telemetry"
)

func TestLossCommandRejectsIncompleteEvidence(t *testing.T) {
	input := telemetry.LossGateInput{Now: time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC), DesiredTaskCount: 2}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"loss"}, bytes.NewReader(raw), &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "{\"state\":\"unknown\"}\n" {
		t.Fatalf("unexpected output %q", output.String())
	}
}

func TestBudgetSnapshotCommandFailsClosedWithoutInventory(t *testing.T) {
	raw := []byte(`{"now":"2026-08-11T12:20:00Z","window_start":"2026-08-11T12:00:00Z","window_end":"2026-08-11T12:10:00Z","expected_cells":["proof-01"],"inventory":{},"eligible":[],"availability_bad":[],"latency_slow":[],"heartbeats":{},"delivery_uncertain":false}`)
	var output bytes.Buffer
	if err := run([]string{"budget-snapshot"}, bytes.NewReader(raw), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"state":"unknown"`) {
		t.Fatalf("unexpected output %q", output.String())
	}
}

func TestStagingObservationCommandFailsClosedWithoutInventory(t *testing.T) {
	raw := []byte(`{"now":"2026-08-11T00:15:00Z","window_start":"2026-08-04T00:00:00Z","window_end":"2026-08-11T00:00:00Z","expected_cells":["staging-01"],"inventory":{},"eligible":[],"availability_bad":[],"latency_slow":[],"heartbeats":{},"delivery_uncertain":false}`)
	var output bytes.Buffer
	if err := run([]string{"staging-observation-snapshot"}, bytes.NewReader(raw), &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "{\"state\":\"unknown\",\"days\":null}\n" {
		t.Fatalf("unexpected output %q", output.String())
	}
}

func TestPromotionCommandProducesCanonicalFailClosedEvidence(t *testing.T) {
	gateNames := []string{"budget", "capacity", "critical_alarms", "infrastructure", "loss", "paging", "privacy", "proof", "staging_observation"}
	gates := make([]telemetry.PromotionGate, 0, len(gateNames))
	for _, name := range gateNames {
		windowStart := "2026-08-04T12:00:00Z"
		if name == "budget" {
			windowStart = "2026-07-12T12:00:00Z"
		}
		gates = append(gates, telemetry.PromotionGate{
			Name: name, State: telemetry.GatePass, ObservedAt: "2026-08-11T12:00:00Z",
			WindowStart: windowStart, WindowEnd: "2026-08-11T12:00:00Z",
		})
	}
	gates[4].State = telemetry.GateUnknown
	request := promotionRequest{
		SourceEnvironment: "staging", TargetEnvironment: "production", EvaluatedAt: "2026-08-11T12:00:00Z",
		WorkflowRunID: "123", WorkflowIdentity: "https://github.com/acctbl/accountable/.github/workflows/promotion.yml@refs/heads/main",
		ReleaseID:             "018f47d2-7f00-7abc-8def-1234567890ab",
		ReleaseManifestDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		SignatureBundleDigest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		Artifacts: []telemetry.ReleaseArtifact{
			{Name: "web_assets", Type: "web_assets", Digest: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", ArchiveDigest: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", ImmutableURI: "s3://releases/web-sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff.tar.gz"},
			{Name: "collector_oci", Type: "oci_image", Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ImmutableURI: "oci://collector@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			{Name: "api_oci", Type: "oci_image", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ImmutableURI: "oci://api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		},
		Gates: gates,
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"promotion"}, bytes.NewReader(raw), &output); err != nil {
		t.Fatal(err)
	}
	evidence, err := telemetry.ParsePromotionEvidence(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Decision != telemetry.GateUnknown || !strings.HasSuffix(output.String(), "\n") {
		t.Fatalf("decision=%s output=%q", evidence.Decision, output.String())
	}
}

func TestPromotionCommandRejectsAStalePassingGate(t *testing.T) {
	gateNames := []string{"budget", "capacity", "critical_alarms", "infrastructure", "loss", "paging", "privacy", "proof", "staging_observation"}
	gates := make([]telemetry.PromotionGate, 0, len(gateNames))
	for _, name := range gateNames {
		windowStart := "2026-08-04T12:00:00Z"
		if name == "budget" {
			windowStart = "2026-07-12T12:00:00Z"
		}
		gates = append(gates, telemetry.PromotionGate{
			Name: name, State: telemetry.GatePass, ObservedAt: "2026-08-11T11:00:00Z",
			WindowStart: windowStart, WindowEnd: "2026-08-11T12:00:00Z",
		})
	}
	request := promotionRequest{
		SourceEnvironment: "staging", TargetEnvironment: "production", EvaluatedAt: "2026-08-11T12:00:00Z",
		WorkflowRunID: "123", WorkflowIdentity: "https://github.com/acctbl/accountable/.github/workflows/promotion.yml@refs/heads/main",
		ReleaseID:             "018f47d2-7f00-7abc-8def-1234567890ab",
		ReleaseManifestDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		SignatureBundleDigest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		Artifacts: []telemetry.ReleaseArtifact{
			{Name: "api_oci", Type: "oci_image", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ImmutableURI: "oci://api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			{Name: "collector_oci", Type: "oci_image", Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ImmutableURI: "oci://collector@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			{Name: "web_assets", Type: "web_assets", Digest: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", ArchiveDigest: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", ImmutableURI: "s3://releases/web-sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff.tar.gz"},
		},
		Gates: gates,
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"promotion"}, bytes.NewReader(raw), &bytes.Buffer{}); err == nil {
		t.Fatal("stale passing evidence was accepted")
	}
}

func TestPromotionCommandCoversEveryDecisionAndIncompleteShape(t *testing.T) {
	tests := map[string]struct {
		mutate   func(*promotionRequest)
		decision telemetry.GateState
		rejected bool
	}{
		"pass": {decision: telemetry.GatePass},
		"fail": {
			mutate: func(request *promotionRequest) {
				request.Gates[0].State = telemetry.GateFail
			},
			decision: telemetry.GateFail,
		},
		"unknown": {
			mutate: func(request *promotionRequest) {
				request.Gates[0].State = telemetry.GateUnknown
			},
			decision: telemetry.GateUnknown,
		},
		"missing gate": {
			mutate: func(request *promotionRequest) {
				request.Gates = request.Gates[:len(request.Gates)-1]
			},
			rejected: true,
		},
		"conflicting gate": {
			mutate: func(request *promotionRequest) {
				request.Gates[len(request.Gates)-1].Name = request.Gates[0].Name
			},
			rejected: true,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			request := completePromotionRequest()
			if test.mutate != nil {
				test.mutate(&request)
			}
			raw, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			err = run([]string{"promotion"}, bytes.NewReader(raw), &output)
			if test.rejected {
				if err == nil {
					t.Fatal("incomplete promotion evidence was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			evidence, err := telemetry.ParsePromotionEvidence(output.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			if evidence.Decision != test.decision {
				t.Fatalf("decision = %s", evidence.Decision)
			}
		})
	}

	if err := run([]string{"promotion"}, strings.NewReader(`{"source_environment":`), &bytes.Buffer{}); err == nil {
		t.Fatal("malformed promotion evidence was accepted")
	}
}

func completePromotionRequest() promotionRequest {
	gateNames := []string{"budget", "capacity", "critical_alarms", "infrastructure", "loss", "paging", "privacy", "proof", "staging_observation"}
	gates := make([]telemetry.PromotionGate, 0, len(gateNames))
	for _, name := range gateNames {
		windowStart := "2026-08-04T12:00:00Z"
		if name == "budget" {
			windowStart = "2026-07-12T12:00:00Z"
		}
		gates = append(gates, telemetry.PromotionGate{
			Name: name, State: telemetry.GatePass, ObservedAt: "2026-08-11T12:00:00Z",
			WindowStart: windowStart, WindowEnd: "2026-08-11T12:00:00Z",
		})
	}
	return promotionRequest{
		SourceEnvironment: "staging", TargetEnvironment: "production", EvaluatedAt: "2026-08-11T12:00:00Z",
		WorkflowRunID: "123", WorkflowIdentity: "https://github.com/acctbl/accountable/.github/workflows/promotion.yml@refs/heads/main",
		ReleaseID:             "018f47d2-7f00-7abc-8def-1234567890ab",
		ReleaseManifestDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		SignatureBundleDigest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		Artifacts: []telemetry.ReleaseArtifact{
			{Name: "api_oci", Type: "oci_image", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ImmutableURI: "oci://api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			{Name: "collector_oci", Type: "oci_image", Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ImmutableURI: "oci://collector@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			{Name: "web_assets", Type: "web_assets", Digest: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", ArchiveDigest: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", ImmutableURI: "s3://releases/web-sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff.tar.gz"},
		},
		Gates: gates,
	}
}
