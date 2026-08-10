package telemetry

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
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

func TestCollectorConfigGolden(t *testing.T) {
	config, err := ParseCollectorDeploymentConfig(readFixture(t, "collector/config/positive/deployment.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := BuildCollectorConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	want := string(readFixture(t, "collector/config/positive/collector.yaml"))
	if got != want {
		t.Fatalf("collector config differs from golden\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestReleaseVerificationDelegatesTrustToCosign(t *testing.T) {
	artifacts := map[string][]byte{
		"api_oci":       []byte("api manifest"),
		"collector_oci": []byte("collector manifest"),
		"web_assets":    []byte("web manifest"),
	}
	manifest := ReleaseManifest{
		SchemaVersion: ContractSchemaVersion,
		ReleaseID:     "018f47d2-7f00-7abc-9def-1234567890ab",
		CreatedAt:     "2026-08-10T10:00:00Z",
		CommitSHA:     "0123456789abcdef0123456789abcdef01234567",
		Artifacts: []ReleaseArtifact{
			{Name: "api_oci", Type: "oci_image", Digest: "sha256:" + sha256Hex(artifacts["api_oci"]), ImmutableURI: "oci://registry.example/api@sha256:" + sha256Hex(artifacts["api_oci"])},
			{Name: "collector_oci", Type: "oci_image", Digest: "sha256:" + sha256Hex(artifacts["collector_oci"]), ImmutableURI: "oci://registry.example/collector@sha256:" + sha256Hex(artifacts["collector_oci"])},
			{Name: "web_assets", Type: "web_assets", Digest: "sha256:" + sha256Hex(artifacts["web_assets"]), ImmutableURI: "s3://artifacts/web/" + sha256Hex(artifacts["web_assets"])},
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
		ArtifactBytes:          artifacts,
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

func TestLossGate(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	input := LossGateInput{
		Now:                     now,
		DesiredTaskCount:        1,
		RunningCollectorTaskIDs: []string{"0123456789abcdef0123456789abcdef"},
		Series: []LossGateSeries{{
			Environment:     "staging",
			CellID:          "stg-a",
			CollectorTaskID: "0123456789abcdef0123456789abcdef",
			Pipeline:        "traces/app",
			Signal:          "traces",
			SampledAt:       now.Add(-time.Minute),
			Counters:        DeliveryCounters{AcceptedItemCount: 100, SentItemCount: 100},
		}},
	}
	if got := EvaluateLossGate(input); got != GatePass {
		t.Fatalf("got %s", got)
	}
	input.Series[0].Counters = DeliveryCounters{AcceptedItemCount: 101, SentItemCount: 100, PermanentlyDroppedItemCount: 1}
	if got := EvaluateLossGate(input); got != GatePass {
		t.Fatalf("sub-one-percent loss got %s", got)
	}
	input.Series[0].Counters = DeliveryCounters{AcceptedItemCount: 100, SentItemCount: 99, PermanentlyDroppedItemCount: 1}
	if got := EvaluateLossGate(input); got != GateFail {
		t.Fatalf("one-percent loss got %s", got)
	}
	input.Series[0].Counters = DeliveryCounters{
		AcceptedItemCount:           101,
		SentItemCount:               100,
		PendingItemCount:            1,
		OldestPendingItemAgeSeconds: 600,
	}
	if got := EvaluateLossGate(input); got != GateFail {
		t.Fatalf("got %s", got)
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
