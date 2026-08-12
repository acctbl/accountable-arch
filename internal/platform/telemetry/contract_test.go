package telemetry

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type blobVerifierFunc func(context.Context, []byte, []byte, ReleaseTrustPolicy) error

func (function blobVerifierFunc) VerifyBlob(ctx context.Context, blob, bundle []byte, policy ReleaseTrustPolicy) error {
	return function(ctx, blob, bundle, policy)
}

func TestCanonicalizeJSONUsesRFC8785(t *testing.T) {
	raw := []byte(`{"numbers":[333333333.33333329,1E30,4.50,2e-3,0.000000000000000000000000001],"string":"€$\u000f\nA'B\"\\\"/","literals":[null,true,false]}`)
	want := `{"literals":[null,true,false],"numbers":[333333333.3333333,1e+30,4.5,0.002,1e-27],"string":"€$\u000f\nA'B\"\\\"/"}`

	got, err := CanonicalizeJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("canonical JSON differs\ngot:  %s\nwant: %s", got, want)
	}
}

func TestContractFixtures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		positive string
		negative string
		parse    func([]byte) error
	}{
		{"client", "client/positive/*.json", "client/negative/*.json", func(raw []byte) error { _, err := ParseClientEnvelope(raw); return err }},
		{"config", "config/positive/*.json", "config/negative/*.json", func(raw []byte) error { _, err := ParseConfigIdentity(raw); return err }},
		{"resource", "resource/positive/*.json", "resource/negative/*.json", func(raw []byte) error { _, err := ParseProcessResource(raw); return err }},
		{"work", "work/positive/*.json", "work/negative/*.json", func(raw []byte) error {
			var value WorkIdentity
			if err := decodeStrictJSON(raw, &value); err != nil {
				return err
			}
			return ValidateRootWorkIdentity(value)
		}},
		{"signal", "signal/positive/*.json", "signal/negative/*.json", func(raw []byte) error { _, err := ParseSignalFixture(raw); return err }},
		{"proof", "proof/positive/*.json", "proof/negative/*.json", func(raw []byte) error { _, err := ParseProofFixture(raw); return err }},
		{"alert", "alert/positive/*.json", "alert/negative/*.json", func(raw []byte) error { _, err := ParseAlertMetadata(raw); return err }},
		{"loss", "loss/positive/*.json", "loss/negative/*.json", func(raw []byte) error { _, err := ParseDeliveryCounters(raw); return err }},
		{"collector", "collector/config/positive/*.json", "collector/config/negative/*.json", func(raw []byte) error { _, err := ParseCollectorDeploymentConfig(raw); return err }},
		{"release", "release/positive/*.json", "release/negative/*.json", func(raw []byte) error { _, err := ParseReleaseManifest(raw); return err }},
		{"inventory", "inventory/positive/*.json", "inventory/negative/*.json", func(raw []byte) error { _, err := ParseInventoryRecord(raw); return err }},
		{"web manifest", "web-manifest/positive/manifest.json", "web-manifest/negative/*.json", func(raw []byte) error { _, err := ParseWebFileManifest(raw); return err }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for _, path := range fixtureFiles(t, test.positive) {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := test.parse(raw); err != nil {
					t.Fatalf("positive fixture %s failed: %v", path, err)
				}
			}
			for _, path := range fixtureFiles(t, test.negative) {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := test.parse(raw); !errors.Is(err, ErrInvalidContract) {
					t.Fatalf("negative fixture %s returned %v", path, err)
				}
			}
		})
	}
}

func TestInventoryChainRequiresOrderAndUniqueDeployments(t *testing.T) {
	active := InventoryRecord{
		SchemaVersion: ContractSchemaVersion,
		Environment:   "staging",
		CellID:        "stg-a",
		State:         "active",
		EffectiveAt:   "2026-08-10T10:00:00Z",
		DeploymentID:  "018f47d2-7f00-7abc-8def-1234567890ab",
	}
	digest, err := InventoryRecordDigest(active)
	if err != nil {
		t.Fatal(err)
	}
	inactive := InventoryRecord{
		SchemaVersion:        ContractSchemaVersion,
		Environment:          "staging",
		CellID:               "stg-a",
		State:                "inactive",
		EffectiveAt:          "2026-08-10T10:05:00Z",
		DeploymentID:         "018f47d2-7f00-7abc-9def-1234567890ab",
		PreviousRecordDigest: digest,
	}
	if err := ValidateInventoryChain([]InventoryRecord{active, inactive}); err != nil {
		t.Fatal(err)
	}
	inactive.EffectiveAt = active.EffectiveAt
	if err := ValidateInventoryChain([]InventoryRecord{active, inactive}); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("unordered chain returned %v", err)
	}
}

func TestWebManifestIncludesOnlyExactRuntimeExclusions(t *testing.T) {
	root := filepath.Join("..", "..", "..", "testdata", "telemetry", "web-manifest", "positive", "data")
	manifest, err := BuildWebFileManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(manifest.Manifest.Files))
	for _, file := range manifest.Manifest.Files {
		paths = append(paths, file.Path)
	}
	want := []string{"_brand/mark.txt", "_runtime/extra.txt", "assets/app.js", "index.html"}
	if len(paths) != len(want) {
		t.Fatalf("got %v", paths)
	}
	for index := range want {
		if paths[index] != want[index] {
			t.Fatalf("got %v", paths)
		}
	}
}

// collectorProofToken stands in for the access-policy token that collector-init
// resolves at run time. No real credential ever reaches a fixture.
const collectorProofToken = "collector-proof-token"

func TestCollectorConfigGolden(t *testing.T) {
	config, err := ParseCollectorDeploymentConfig(readFixture(t, "collector/config/positive/deployment.json"))
	if err != nil {
		t.Fatal(err)
	}
	config.GrafanaToken = collectorProofToken
	got, err := BuildCollectorConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	want := string(readFixture(t, "collector/config/positive/collector.yaml"))
	if got != want {
		t.Fatalf("collector config differs from golden\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestScratchWriterBounds(t *testing.T) {
	root := t.TempDir()
	writer, err := NewScratchWriter(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Write("collector.yaml", []byte("receivers:\n")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(root, "collector.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != ScratchFileMode {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
	rejected := map[string][]byte{
		"../escape":     []byte("x"),
		"nested/file":   []byte("x"),
		"":              []byte("x"),
		"oversized.bin": make([]byte, MaxScratchFileBytes+1),
	}
	for name, contents := range rejected {
		if err := writer.Write(name, contents); !errors.Is(err, ErrInvalidContract) {
			t.Errorf("write %q returned %v", name, err)
		}
	}

	link := filepath.Join(root, "linked")
	if err := os.Symlink(filepath.Join(root, "collector.yaml"), link); err != nil {
		t.Fatal(err)
	}
	if err := writer.Write("linked", []byte("x")); err == nil {
		t.Error("writing through a symlink was permitted")
	}
	if _, err := NewScratchWriter(filepath.Join(root, "collector.yaml")); !errors.Is(err, ErrInvalidContract) {
		t.Errorf("file root returned %v", err)
	}
}

func TestCollectorConfigEnforcesSignalContract(t *testing.T) {
	config, err := ParseCollectorDeploymentConfig(readFixture(t, "collector/config/positive/deployment.json"))
	if err != nil {
		t.Fatal(err)
	}
	config.GrafanaToken = collectorProofToken
	rendered, err := BuildCollectorConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	required := []string{
		"username: '1765140'",
		`endpoint: 0.0.0.0:4318`,
		`endpoint: 0.0.0.0:4319`,
		`host: '127.0.0.1'`,
		`port: 8888`,
		"accountable_emf/sli:",
		"region: eu-west-2",
		"mode: sli",
		`set(name, "EligibleRequestCount") where name == "accountable.sli.request.eligible"`,
		`set(attributes["ServiceName"], "accountable-api")`,
		`set(attributes["deployment.environment.name"], "development")`,
		`resource.attributes["accountable.cell.id"] != "dev-a"`,
		"namespace: Accountable/SLO",
		`'Len(attributes) > 0'`,
		"'is_monotonic != true'",
	}
	for _, fragment := range required {
		if !strings.Contains(rendered, fragment) {
			t.Errorf("rendered config is missing %q", fragment)
		}
	}
	forbidden := []string{"awsemf", "debug:", "logging:", "verbosity", "0.0.0.0:8888", "resource_to_telemetry_conversion", "otelcol_RefusedItemCount_total"}
	for _, fragment := range forbidden {
		if strings.Contains(rendered, fragment) {
			t.Errorf("rendered config contains %q", fragment)
		}
	}
	sliPipeline := pipelineProcessors(t, rendered, "metrics/slo")
	if strings.Contains(sliPipeline, "batch") {
		t.Errorf("the SLI pipeline must not batch: %s", sliPipeline)
	}
	if !strings.Contains(sliPipeline, "filter/sli") || !strings.Contains(sliPipeline, "transform/sli") {
		t.Errorf("the SLI pipeline must validate and rename points: %s", sliPipeline)
	}
	for _, pipeline := range []string{"traces/app", "metrics/app", "logs/app"} {
		processors := pipelineProcessors(t, rendered, pipeline)
		for _, required := range []string{"memory_limiter", "filter/identity", "transform/identity", "transform/allowlist", "batch"} {
			if !strings.Contains(processors, required) {
				t.Errorf("pipeline %s is missing %s: %s", pipeline, required, processors)
			}
		}
	}
	if identity := pipelineProcessors(t, rendered, "metrics/app"); !strings.Contains(identity, "transform/runtime_identity_labels") {
		t.Errorf("the Grafana metrics pipeline must copy runtime identity: %s", identity)
	}
	if labels := pipelineProcessors(t, rendered, "traces/app"); strings.Contains(labels, "transform/runtime_identity_labels") {
		t.Errorf("only metrics carry identity labels: %s", labels)
	}
}

func TestCollectorAllowListRejectsUnregisteredAttributes(t *testing.T) {
	metricAttributes, err := RegisteredSignalAttributes("metric")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range metricAttributes {
		if _, workIdentity := workIdentityFields[name]; workIdentity {
			t.Errorf("metric allow-list contains work identity %q", name)
		}
	}
	spanAttributes, err := RegisteredSignalAttributes("trace")
	if err != nil {
		t.Fatal(err)
	}
	if contains(spanAttributes, "trace_id") || contains(spanAttributes, "span_id") {
		t.Error("span allow-list duplicates native trace identity")
	}
	if _, err := RegisteredSignalAttributes("unknown"); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("unknown signal kind returned %v", err)
	}
}

func pipelineProcessors(t *testing.T, rendered, pipeline string) string {
	t.Helper()
	marker := "    " + pipeline + ":\n"
	start := strings.Index(rendered, marker)
	if start < 0 {
		t.Fatalf("pipeline %s is absent", pipeline)
	}
	section := rendered[start+len(marker):]
	const processorPrefix = "      processors: "
	offset := strings.Index(section, processorPrefix)
	if offset < 0 {
		t.Fatalf("pipeline %s has no processors", pipeline)
	}
	line := section[offset+len(processorPrefix):]
	return line[:strings.Index(line, "\n")]
}

func TestReleaseVerificationDelegatesTrustToCosign(t *testing.T) {
	artifacts := map[string]string{
		"api_oci":       "sha256:" + sha256Hex([]byte("api manifest")),
		"collector_oci": "sha256:" + sha256Hex([]byte("collector manifest")),
		"web_assets":    "sha256:" + sha256Hex([]byte("web manifest")),
	}
	manifest := ReleaseManifest{
		SchemaVersion: ContractSchemaVersion,
		ReleaseID:     "018f47d2-7f00-7abc-9def-1234567890ab",
		CreatedAt:     "2026-08-10T10:00:00Z",
		CommitSHA:     "0123456789abcdef0123456789abcdef01234567",
		Artifacts: []ReleaseArtifact{
			{Name: "api_oci", Type: "oci_image", Digest: artifacts["api_oci"], ImmutableURI: "oci://registry.example/api@" + artifacts["api_oci"]},
			{Name: "collector_oci", Type: "oci_image", Digest: artifacts["collector_oci"], ImmutableURI: "oci://registry.example/collector@" + artifacts["collector_oci"]},
			{Name: "web_assets", Type: "web_assets", Digest: artifacts["web_assets"], ArchiveDigest: "sha256:" + strings.Repeat("e", 64), ImmutableURI: "s3://artifacts/web/sha256:" + strings.Repeat("e", 64) + ".tar.gz"},
		},
	}
	produced, err := ProduceReleaseManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	bundle := []byte(`{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json"}`)
	verifierCalled := false
	verifier := blobVerifierFunc(func(_ context.Context, blob, gotBundle []byte, policy ReleaseTrustPolicy) error {
		verifierCalled = true
		if string(blob) != string(produced.CanonicalManifest) || string(gotBundle) != string(bundle) ||
			policy.OIDCIssuer != GitHubActionsOIDCIssuer {
			t.Fatal("unexpected verifier input")
		}
		return nil
	})
	err = VerifyRelease(context.Background(), VerifyReleaseInput{
		ManifestJSON:           produced.CanonicalManifest,
		BundleJSON:             bundle,
		ExpectedManifestDigest: produced.ManifestDigest,
		ExpectedBundleDigest:   "sha256:" + sha256Hex(bundle),
		ArtifactDigests:        artifacts,
		Policy: ReleaseTrustPolicy{
			OIDCIssuer:       GitHubActionsOIDCIssuer,
			WorkflowIdentity: "https://github.com/acctbl/accountable/.github/workflows/release.yml@refs/heads/main",
		},
	}, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if !verifierCalled {
		t.Fatal("signature verifier was not called")
	}
}

func TestCosignCommandUsesExactIdentityPolicy(t *testing.T) {
	called := false
	verifier := CommandBlobSignatureVerifier{
		Binary: "/tools/cosign-v3.0.6",
		Run: func(_ context.Context, name string, arguments ...string) error {
			called = true
			if name != "/tools/cosign-v3.0.6" || len(arguments) != 8 ||
				arguments[0] != "verify-blob" || arguments[1] != "--bundle" ||
				arguments[3] != "--certificate-identity" || arguments[5] != "--certificate-oidc-issuer" ||
				arguments[6] != GitHubActionsOIDCIssuer {
				t.Fatalf("unexpected command: %s %v", name, arguments)
			}
			if got, err := os.ReadFile(arguments[2]); err != nil || string(got) != "bundle" {
				t.Fatalf("bundle: %q, %v", got, err)
			}
			if got, err := os.ReadFile(arguments[7]); err != nil || string(got) != "manifest" {
				t.Fatalf("manifest: %q, %v", got, err)
			}
			return nil
		},
	}
	err := verifier.VerifyBlob(context.Background(), []byte("manifest"), []byte("bundle"), ReleaseTrustPolicy{
		OIDCIssuer:       GitHubActionsOIDCIssuer,
		WorkflowIdentity: "https://github.com/acctbl/accountable/.github/workflows/release.yml@refs/heads/main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("cosign command was not called")
	}
}

func TestClientAdmissionUsesCompleteIdentityTuple(t *testing.T) {
	raw := readFixture(t, "client/positive/app-ready.json")
	envelope, err := ParseClientEnvelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	active := ClientIdentityTuple{
		Application:                envelope.ClientApplication,
		ReleaseID:                  envelope.ReleaseID,
		ArtifactType:               envelope.ClientArtifactType,
		ArtifactDigest:             envelope.ClientArtifactDigest,
		ConfigurationSchemaVersion: envelope.ConfigurationSchemaVersion,
		ConfigurationRevision:      envelope.ConfigurationRevision,
		ConfigurationFingerprint:   envelope.ConfigurationFingerprint,
	}
	policy := ClientAdmissionPolicy{Now: time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC), ActiveIdentity: active}
	if err := ValidateClientAdmission(envelope, policy); err != nil {
		t.Fatal(err)
	}
	policy.ActiveIdentity.ArtifactDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	if err := ValidateClientAdmission(envelope, policy); !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("mixed identity returned %v", err)
	}
}

type parityVectors struct {
	SchemaVersion int `json:"schema_version"`
	Sampling      []struct {
		CorrelationID string `json:"correlation_id"`
		EventName     string `json:"event_name"`
		Sampled       bool   `json:"sampled"`
	} `json:"sampling"`
	AdmissionWindow []struct {
		Now        string `json:"now"`
		OccurredAt string `json:"occurred_at"`
		Admitted   bool   `json:"admitted"`
	} `json:"admission_window"`
}

func TestClientParityVectors(t *testing.T) {
	var vectors parityVectors
	if err := decodeStrictJSON(readFixture(t, "client/parity/vectors.json"), &vectors); err != nil {
		t.Fatal(err)
	}
	if vectors.SchemaVersion != ContractSchemaVersion || len(vectors.Sampling) == 0 || len(vectors.AdmissionWindow) == 0 {
		t.Fatal("parity vectors are incomplete")
	}
	for _, vector := range vectors.Sampling {
		sampled, err := ClientSampled(vector.CorrelationID, vector.EventName)
		if err != nil {
			t.Fatal(err)
		}
		if sampled != vector.Sampled {
			t.Fatalf("sampling %s %s = %t", vector.CorrelationID, vector.EventName, sampled)
		}
	}

	envelope, err := ParseClientEnvelope(readFixture(t, "client/positive/app-ready.json"))
	if err != nil {
		t.Fatal(err)
	}
	active := ClientIdentityTuple{
		Application:                envelope.ClientApplication,
		ReleaseID:                  envelope.ReleaseID,
		ArtifactType:               envelope.ClientArtifactType,
		ArtifactDigest:             envelope.ClientArtifactDigest,
		ConfigurationSchemaVersion: envelope.ConfigurationSchemaVersion,
		ConfigurationRevision:      envelope.ConfigurationRevision,
		ConfigurationFingerprint:   envelope.ConfigurationFingerprint,
	}
	for _, vector := range vectors.AdmissionWindow {
		now, err := time.Parse("2006-01-02T15:04:05.000Z", vector.Now)
		if err != nil {
			t.Fatal(err)
		}
		envelope.OccurredAt = vector.OccurredAt
		err = ValidateClientAdmission(envelope, ClientAdmissionPolicy{Now: now, ActiveIdentity: active})
		if (err == nil) != vector.Admitted {
			t.Fatalf("admission %s at %s returned %v", vector.OccurredAt, vector.Now, err)
		}
	}
}

func TestProofVerification(t *testing.T) {
	key := base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	timestamp := "1786356000"
	nonce := "018f47d2-7f00-7abc-8def-1234567890ab"
	signature, err := SignProof(key, timestamp, nonce, []byte(ProofRequestBody))
	if err != nil {
		t.Fatal(err)
	}
	err = VerifyProof(ProofVerification{
		Headers:    ProofHeaders{Timestamp: timestamp, Nonce: nonce, Signature: signature},
		Body:       []byte(ProofRequestBody),
		ServerTime: time.Unix(1786356000, 0),
		ActiveKey:  key,
	})
	if err != nil {
		t.Fatal(err)
	}
}

const (
	firstCollectorTaskID  = "0123456789abcdef0123456789abcdef"
	secondCollectorTaskID = "fedcba9876543210fedcba9876543210"
)

func lossGateInput(now time.Time, taskIDs ...string) LossGateInput {
	input := LossGateInput{
		Now:                     now,
		DesiredTaskCount:        len(taskIDs),
		RunningCollectorTaskIDs: taskIDs,
	}
	for _, signal := range GrafanaDeliverySignals() {
		input.Terminal = append(input.Terminal, TerminalDelivery{
			GrafanaDeliverySignal: signal,
			SentItemCount:         100,
		})
		for _, taskID := range taskIDs {
			input.Pending = append(input.Pending, PendingDelivery{
				GrafanaDeliverySignal: signal,
				CollectorTaskID:       taskID,
				SampledAt:             now.Add(-time.Minute),
			})
		}
	}
	return input
}

func TestLossGateTerminalOutcomes(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	input := lossGateInput(now, firstCollectorTaskID)
	if got := EvaluateLossGate(input); got != GatePass {
		t.Fatalf("healthy gate got %s", got)
	}

	input = lossGateInput(now, firstCollectorTaskID)
	input.Terminal[0] = TerminalDelivery{
		GrafanaDeliverySignal:       input.Terminal[0].GrafanaDeliverySignal,
		SentItemCount:               100,
		PermanentlyDroppedItemCount: 1,
	}
	if got := EvaluateLossGate(input); got != GatePass {
		t.Fatalf("sub-one-percent loss got %s", got)
	}

	input = lossGateInput(now, firstCollectorTaskID)
	input.Terminal[0] = TerminalDelivery{
		GrafanaDeliverySignal:       input.Terminal[0].GrafanaDeliverySignal,
		SentItemCount:               99,
		PermanentlyDroppedItemCount: 1,
	}
	if got := EvaluateLossGate(input); got != GateFail {
		t.Fatalf("one-percent loss got %s", got)
	}

	input = lossGateInput(now, firstCollectorTaskID)
	input.Terminal[1].RefusedItemCount = 1
	if got := EvaluateLossGate(input); got != GateFail {
		t.Fatalf("refusal got %s", got)
	}

	input = lossGateInput(now, firstCollectorTaskID)
	input.Terminal[2].SentItemCount = 99
	if got := EvaluateLossGate(input); got != GateFail {
		t.Fatalf("thin denominator got %s", got)
	}
}

func TestLossGateAggregatesTerminalItemsAcrossTasks(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	input := lossGateInput(now, firstCollectorTaskID, secondCollectorTaskID)
	if got := EvaluateLossGate(input); got != GatePass {
		t.Fatalf("replaced task with a shared denominator got %s", got)
	}
}

func TestLossGateIsUnknownWithoutCompleteFreshEvidence(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	tests := map[string]func(*LossGateInput){
		"silent task": func(input *LossGateInput) {
			input.Pending = input.Pending[1:]
		},
		"stale task sample": func(input *LossGateInput) {
			input.Pending[0].SampledAt = now.Add(-181 * time.Second)
		},
		"future task sample": func(input *LossGateInput) {
			input.Pending[0].SampledAt = now.Add(time.Second)
		},
		"unknown task": func(input *LossGateInput) {
			input.Pending[0].CollectorTaskID = secondCollectorTaskID
		},
		"duplicated task series": func(input *LossGateInput) {
			input.Pending = append(input.Pending, input.Pending[0])
		},
		"missing terminal signal": func(input *LossGateInput) {
			input.Terminal = input.Terminal[1:]
		},
		"duplicated terminal signal": func(input *LossGateInput) {
			input.Terminal = append(input.Terminal, input.Terminal[0])
		},
		"task count mismatch": func(input *LossGateInput) {
			input.DesiredTaskCount = 2
		},
		"task list failure": func(input *LossGateInput) {
			input.TaskListFailed = true
		},
		"delivery uncertainty": func(input *LossGateInput) {
			input.DeliveryUncertain = true
		},
		"accounting uncertainty": func(input *LossGateInput) {
			input.AccountingUncertain = true
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input := lossGateInput(now, firstCollectorTaskID)
			mutate(&input)
			if got := EvaluateLossGate(input); got != GateUnknown {
				t.Fatalf("got %s", got)
			}
		})
	}
}

func TestLossGateFailsOnStuckQueue(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	input := lossGateInput(now, firstCollectorTaskID, secondCollectorTaskID)
	input.Pending[1].PendingItemCount = 5
	input.Pending[1].OldestPendingItemAgeSeconds = 600
	if got := EvaluateLossGate(input); got != GateFail {
		t.Fatalf("stuck queue on one task got %s", got)
	}
}

func TestBudgetIgnoresPeriodsWithNoActiveCell(t *testing.T) {
	windowStart := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	eligible := int64(100)
	result := EvaluateBudget(BudgetInput{
		Now:         windowStart.Add(25 * time.Minute),
		WindowStart: windowStart,
		WindowEnd:   windowStart.Add(10 * time.Minute),
		Periods: []BudgetPeriod{
			{StartedAt: windowStart},
			{StartedAt: windowStart.Add(5 * time.Minute), Eligible: &eligible, RequiredCells: []string{"proof-01"}, HeartbeatCells: []string{"proof-01"}},
		},
	})
	if result.State != BudgetProvisional || result.Eligible != eligible {
		t.Fatalf("budget result = %+v", result)
	}
}

func TestBudgetSnapshotJoinsInventoryHeartbeatsAndIndicatorPoints(t *testing.T) {
	windowStart := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	active := InventoryRecord{
		SchemaVersion: ContractSchemaVersion, Environment: "development", CellID: "proof-01", State: "active",
		EffectiveAt: windowStart.Format(time.RFC3339), DeploymentID: "018f47d2-7f00-7abc-8def-1234567890ab",
	}
	activeDigest, err := InventoryRecordDigest(active)
	if err != nil {
		t.Fatal(err)
	}
	inactive := InventoryRecord{
		SchemaVersion: ContractSchemaVersion, Environment: "development", CellID: "proof-01", State: "inactive",
		EffectiveAt: windowStart.Add(5 * time.Minute).Format(time.RFC3339), DeploymentID: "018f47d2-7f00-7abc-8def-1234567890ac",
		PreviousRecordDigest: activeDigest,
	}
	result := EvaluateBudgetSnapshot(BudgetSnapshot{
		Now: windowStart.Add(25 * time.Minute), WindowStart: windowStart, WindowEnd: windowStart.Add(10 * time.Minute),
		ExpectedCells: []string{"proof-01"},
		Inventory:     map[string][]InventoryRecord{"proof-01": {active, inactive}},
		Eligible:      []BudgetMetricPoint{{StartedAt: windowStart, Value: 100}},
		Heartbeats:    map[string][]BudgetMetricPoint{"proof-01": {{StartedAt: windowStart, Value: 30}}},
	})
	if result.State != BudgetProvisional || result.Eligible != 100 || result.AvailabilityRemainingBasisPoints != 10000 {
		t.Fatalf("budget snapshot result = %+v", result)
	}
}

func TestStagingObservationRequiresOneHundredEligibleRequestsOnEveryCompleteUTCDay(t *testing.T) {
	windowStart := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	input := completeStagingObservation(windowStart)
	result := EvaluateStagingObservation(input)
	if result.State != GatePass || len(result.Days) != 7 {
		t.Fatalf("staging observation result = %+v", result)
	}
	for _, day := range result.Days {
		if day.Eligible != 100 {
			t.Fatalf("daily eligible requests = %d", day.Eligible)
		}
	}

	input.Periods[2*24*12].Eligible = nil
	result = EvaluateStagingObservation(input)
	if result.State != GateFail {
		t.Fatalf("low-traffic staging observation state = %s", result.State)
	}
}

func TestStagingObservationFailsClosedOnIncompleteEvidence(t *testing.T) {
	windowStart := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	tests := map[string]func(*BudgetInput){
		"non-UTC boundary": func(input *BudgetInput) {
			input.WindowStart = input.WindowStart.Add(time.Hour)
		},
		"delivery uncertainty": func(input *BudgetInput) {
			input.DeliveryUncertain = true
		},
		"missing heartbeat": func(input *BudgetInput) {
			input.Periods[0].HeartbeatCells = nil
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input := completeStagingObservation(windowStart)
			mutate(&input)
			if result := EvaluateStagingObservation(input); result.State != GateUnknown {
				t.Fatalf("staging observation result = %+v", result)
			}
		})
	}
}

func completeStagingObservation(windowStart time.Time) BudgetInput {
	const periodsPerDay = 24 * 12
	periods := make([]BudgetPeriod, 7*periodsPerDay)
	for index := range periods {
		periods[index] = BudgetPeriod{
			StartedAt:      windowStart.Add(time.Duration(index) * 5 * time.Minute),
			RequiredCells:  []string{"staging-01"},
			HeartbeatCells: []string{"staging-01"},
		}
		if index%periodsPerDay == 0 {
			eligible := int64(100)
			periods[index].Eligible = &eligible
		}
	}
	windowEnd := windowStart.Add(7 * 24 * time.Hour)
	return BudgetInput{
		Now:         windowEnd.Add(15 * time.Minute),
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
		Periods:     periods,
	}
}

func fixtureFiles(t *testing.T, pattern string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "testdata", "telemetry", pattern))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatalf("no fixtures matched %s", pattern)
	}
	return paths
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "telemetry", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
