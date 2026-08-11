package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/acctbl/accountable/internal/platform/clock"
	"github.com/acctbl/accountable/internal/platform/telemetry"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New(
			"usage: release <produce|verify|web-manifest|verify-web|web-release|runtime-release> [flags]",
		)
	}
	switch args[0] {
	case "produce":
		return produce(args[1:])
	case "verify":
		return verify(ctx, args[1:])
	case "web-manifest":
		return webManifest(args[1:])
	case "verify-web":
		return verifyWeb(args[1:])
	case "web-release":
		return webRelease(args[1:])
	case "runtime-release":
		return runtimeRelease(args[1:])
	default:
		return fmt.Errorf("unknown release command %q", args[0])
	}
}

// produce builds the canonical manifest for one release. The release identity is
// minted here rather than supplied, so no caller can name an existing release.
func produce(args []string) error {
	set := flag.NewFlagSet("produce", flag.ContinueOnError)
	apiDigest := set.String("api-digest", "", "API image manifest digest")
	collectorDigest := set.String("collector-digest", "", "Collector image manifest digest")
	webDigest := set.String("web-digest", "", "web asset file-set digest")
	webArchiveDigest := set.String("web-archive-digest", "", "web asset archive digest")
	commit := set.String("commit", "", "commit that produced the artifacts")
	registry := set.String("registry", "", "OCI registry hosting both images")
	bucket := set.String("bucket", "", "bucket holding the web file manifest")
	output := set.String("output", "", "absolute path of the canonical manifest")
	if err := set.Parse(args); err != nil {
		return err
	}
	if !filepath.IsAbs(*output) {
		return errors.New("--output must be an absolute path")
	}
	releaseID, err := telemetry.NewWorkID()
	if err != nil {
		return err
	}
	manifest := telemetry.ReleaseManifest{
		SchemaVersion: telemetry.ContractSchemaVersion,
		ReleaseID:     releaseID,
		CreatedAt:     clock.System{}.Now().Format(time.RFC3339),
		CommitSHA:     *commit,
		Artifacts: []telemetry.ReleaseArtifact{
			{
				Name: "api_oci", Type: "oci_image", Digest: *apiDigest,
				ImmutableURI: fmt.Sprintf("oci://%s/accountable@%s", *registry, *apiDigest),
			},
			{
				Name: "collector_oci", Type: "oci_image", Digest: *collectorDigest,
				ImmutableURI: fmt.Sprintf("oci://%s/accountable-collector@%s", *registry, *collectorDigest),
			},
			{
				Name: "web_assets", Type: "web_assets", Digest: *webDigest, ArchiveDigest: *webArchiveDigest,
				ImmutableURI: fmt.Sprintf("s3://%s/releases/v1/web/%s.tar.gz", *bucket, *webArchiveDigest),
			},
		},
	}
	produced, err := telemetry.ProduceReleaseManifest(manifest)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*output, produced.CanonicalManifest, 0o600); err != nil {
		return err
	}
	key, err := telemetry.ReleaseManifestKey(produced.ManifestDigest)
	if err != nil {
		return err
	}
	return emit(map[string]string{
		"release_id":              manifest.ReleaseID,
		"release_manifest_digest": produced.ManifestDigest,
		"release_manifest_key":    key,
	})
}

func verify(ctx context.Context, args []string) error {
	set := flag.NewFlagSet("verify", flag.ContinueOnError)
	manifestPath := set.String("manifest", "", "canonical manifest path")
	bundlePath := set.String("bundle", "", "signature bundle path")
	manifestDigest := set.String("manifest-digest", "", "expected manifest digest")
	bundleDigest := set.String("bundle-digest", "", "expected bundle digest")
	artifactsPath := set.String("artifacts", "", "observed artifact digests path")
	workflowIdentity := set.String("workflow-identity", "", "trusted release workflow identity")
	if err := set.Parse(args); err != nil {
		return err
	}
	manifestJSON, err := readFile(*manifestPath)
	if err != nil {
		return err
	}
	bundleJSON, err := readFile(*bundlePath)
	if err != nil {
		return err
	}
	artifactsJSON, err := readFile(*artifactsPath)
	if err != nil {
		return err
	}
	var digests map[string]string
	if err := json.Unmarshal(artifactsJSON, &digests); err != nil {
		return err
	}
	input := telemetry.VerifyReleaseInput{
		ManifestJSON:           manifestJSON,
		BundleJSON:             bundleJSON,
		ExpectedManifestDigest: *manifestDigest,
		ExpectedBundleDigest:   *bundleDigest,
		ArtifactDigests:        digests,
		Policy: telemetry.ReleaseTrustPolicy{
			OIDCIssuer:       telemetry.GitHubActionsOIDCIssuer,
			WorkflowIdentity: *workflowIdentity,
		},
	}
	if err := telemetry.VerifyRelease(ctx, input, telemetry.CommandBlobSignatureVerifier{}); err != nil {
		return err
	}
	manifest, err := telemetry.ParseReleaseManifest(manifestJSON)
	if err != nil {
		return err
	}
	return emit(map[string]string{
		"release_id":              manifest.ReleaseID,
		"release_manifest_digest": input.ExpectedManifestDigest,
		"verified":                "true",
	})
}

func webManifest(args []string) error {
	set := flag.NewFlagSet("web-manifest", flag.ContinueOnError)
	root := set.String("root", "", "web asset directory")
	output := set.String("output", "", "canonical file manifest path")
	if err := set.Parse(args); err != nil {
		return err
	}
	produced, err := telemetry.BuildWebFileManifest(*root)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*output, produced.CanonicalJSON, 0o600); err != nil {
		return err
	}
	return emit(map[string]string{"web_asset_digest": produced.WebAssetDigest})
}

// verifyWeb proves that an extracted file set is byte-for-byte the signed web
// artifact before any of it is published.
func verifyWeb(args []string) error {
	set := flag.NewFlagSet("verify-web", flag.ContinueOnError)
	root := set.String("root", "", "extracted web asset directory")
	manifestPath := set.String("manifest", "", "signed web file manifest path")
	digest := set.String("digest", "", "signed web asset digest")
	if err := set.Parse(args); err != nil {
		return err
	}
	manifest, err := readFile(*manifestPath)
	if err != nil {
		return err
	}
	if err := telemetry.VerifyWebFileManifest(*root, manifest, *digest); err != nil {
		return err
	}
	return emit(map[string]string{"web_asset_digest": *digest, "verified": "true"})
}

// webRelease writes the runtime release document the browser reads. It carries
// the web asset identity, never a server image digest.
func webRelease(args []string) error {
	set := flag.NewFlagSet("web-release", flag.ContinueOnError)
	releaseID := set.String("release-id", "", "verified release identity")
	manifestDigest := set.String("manifest-digest", "", "verified manifest digest")
	webDigest := set.String("web-digest", "", "verified web asset digest")
	output := set.String("output", "", "runtime release document path")
	if err := set.Parse(args); err != nil {
		return err
	}
	document, err := telemetry.BuildWebRuntimeRelease(*releaseID, *manifestDigest, *webDigest)
	if err != nil {
		return err
	}
	return os.WriteFile(*output, document, 0o644) //nolint:gosec // the browser reads this document
}

func runtimeRelease(args []string) error {
	set := flag.NewFlagSet("runtime-release", flag.ContinueOnError)
	releaseID := set.String("release-id", "", "verified release identity")
	manifestDigest := set.String("manifest-digest", "", "verified manifest digest")
	serviceVersion := set.String("service-version", "", "deployed image manifest digest")
	webDigest := set.String("web-digest", "", "verified web asset digest")
	output := set.String("output", "", "runtime release document path")
	if err := set.Parse(args); err != nil {
		return err
	}
	release := telemetry.RuntimeRelease{
		SchemaVersion:         telemetry.ContractSchemaVersion,
		ReleaseID:             *releaseID,
		ReleaseManifestDigest: *manifestDigest,
		ServiceVersion:        *serviceVersion,
		WebAssetDigest:        *webDigest,
	}
	if err := telemetry.ValidateRuntimeRelease(release); err != nil {
		return err
	}
	document, err := json.Marshal(release)
	if err != nil {
		return err
	}
	return os.WriteFile(*output, document, 0o600)
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(filepath.Clean(path)) //nolint:gosec // release inputs are protected workflow paths
}

func emit(values map[string]string) error {
	encoded, err := json.Marshal(values)
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}
