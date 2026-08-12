package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

var releaseArtifactTypes = map[string]string{
	"api_oci":       "oci_image",
	"collector_oci": "oci_image",
	"web_assets":    "web_assets",
}

type ReleaseArtifact struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	Digest        string `json:"digest"`
	ArchiveDigest string `json:"archive_digest,omitempty"`
	ImmutableURI  string `json:"immutable_uri"`
}

type ReleaseManifest struct {
	SchemaVersion int               `json:"schema_version"`
	ReleaseID     string            `json:"release_id"`
	CreatedAt     string            `json:"created_at"`
	CommitSHA     string            `json:"commit_sha"`
	Artifacts     []ReleaseArtifact `json:"artifacts"`
}

type ProducedRelease struct {
	CanonicalManifest []byte
	ManifestDigest    string
}

type VerifyReleaseInput struct {
	ManifestJSON           []byte
	BundleJSON             []byte
	ExpectedManifestDigest string
	ExpectedBundleDigest   string
	// ArtifactDigests are the digests the deployment observed for each registered
	// artifact name: an OCI manifest digest for an image and the canonical file-set
	// digest for web assets.
	ArtifactDigests map[string]string
	Policy          ReleaseTrustPolicy
}

func ParseReleaseManifest(raw []byte) (ReleaseManifest, error) {
	var manifest ReleaseManifest
	if err := decodeStrictJSON(raw, &manifest); err != nil {
		return ReleaseManifest{}, err
	}
	if err := ValidateReleaseManifest(manifest); err != nil {
		return ReleaseManifest{}, err
	}
	return manifest, nil
}

func ValidateReleaseManifest(manifest ReleaseManifest) error {
	if manifest.SchemaVersion != ContractSchemaVersion || !uuidV7Pattern.MatchString(manifest.ReleaseID) ||
		!validWholeSecondUTC(manifest.CreatedAt) || !commitSHAPattern.MatchString(manifest.CommitSHA) ||
		len(manifest.Artifacts) != len(releaseArtifactTypes) {
		return contractError("release manifest")
	}
	seen := make(map[string]struct{}, len(manifest.Artifacts))
	var apiDigest string
	var webDigest string
	for _, artifact := range manifest.Artifacts {
		expectedType, ok := releaseArtifactTypes[artifact.Name]
		if !ok || artifact.Type != expectedType || !digestPattern.MatchString(artifact.Digest) ||
			!immutableURIPattern.MatchString(artifact.ImmutableURI) {
			return contractError("release artifact")
		}
		if _, duplicate := seen[artifact.Name]; duplicate {
			return contractError("release artifact")
		}
		seen[artifact.Name] = struct{}{}
		if artifact.Type == "oci_image" && !bytes.Contains([]byte(artifact.ImmutableURI), []byte("@"+artifact.Digest)) {
			return contractError("OCI artifact URI")
		}
		if artifact.Type == "oci_image" && artifact.ArchiveDigest != "" {
			return contractError("OCI archive digest")
		}
		if artifact.Name == "web_assets" && (!digestPattern.MatchString(artifact.ArchiveDigest) ||
			!bytes.Contains([]byte(artifact.ImmutableURI), []byte(artifact.ArchiveDigest))) {
			return contractError("web archive digest")
		}
		if artifact.Name == "api_oci" {
			apiDigest = artifact.Digest
		}
		if artifact.Name == "web_assets" {
			webDigest = artifact.Digest
		}
	}
	if apiDigest == webDigest {
		return contractError("artifact identity separation")
	}
	return nil
}

func ProduceReleaseManifest(manifest ReleaseManifest) (ProducedRelease, error) {
	if err := ValidateReleaseManifest(manifest); err != nil {
		return ProducedRelease{}, err
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return ProducedRelease{}, err
	}
	canonical, err := CanonicalizeJSON(raw)
	if err != nil {
		return ProducedRelease{}, fmt.Errorf("canonicalize release manifest: %w", err)
	}
	return ProducedRelease{
		CanonicalManifest: canonical,
		ManifestDigest:    "sha256:" + sha256Hex(canonical),
	}, nil
}

func VerifyRelease(ctx context.Context, input VerifyReleaseInput, verifier BlobSignatureVerifier) error {
	if verifier == nil ||
		!digestPattern.MatchString(input.ExpectedManifestDigest) || !digestPattern.MatchString(input.ExpectedBundleDigest) {
		return contractError("release verification input")
	}
	manifest, err := ParseReleaseManifest(input.ManifestJSON)
	if err != nil {
		return err
	}
	canonical, err := CanonicalizeJSON(input.ManifestJSON)
	if err != nil {
		return contractError("release canonicalization")
	}
	if "sha256:"+sha256Hex(canonical) != input.ExpectedManifestDigest ||
		"sha256:"+sha256Hex(input.BundleJSON) != input.ExpectedBundleDigest {
		return contractError("release digest")
	}
	if err := verifier.VerifyBlob(ctx, canonical, input.BundleJSON, input.Policy); err != nil {
		return err
	}
	if len(input.ArtifactDigests) != len(manifest.Artifacts) {
		return contractError("release artifacts")
	}
	for _, artifact := range manifest.Artifacts {
		observed, ok := input.ArtifactDigests[artifact.Name]
		if !ok || observed != artifact.Digest {
			return contractError("release artifact digest")
		}
	}
	return nil
}

func ReleaseManifestKey(digest string) (string, error) {
	if !digestPattern.MatchString(digest) {
		return "", contractError("release manifest digest")
	}
	return "releases/v1/manifests/" + digest + ".json", nil
}

func ReleaseBundleKey(digest string) (string, error) {
	if !digestPattern.MatchString(digest) {
		return "", contractError("release bundle digest")
	}
	return "releases/v1/bundles/" + digest + ".json", nil
}
