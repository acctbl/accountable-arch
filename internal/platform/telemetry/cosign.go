package telemetry

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
)

type ReleaseTrustPolicy struct {
	OIDCIssuer       string
	WorkflowIdentity string
}

type BlobSignatureVerifier interface {
	VerifyBlob(context.Context, []byte, []byte, ReleaseTrustPolicy) error
}

type CommandBlobSignatureVerifier struct {
	Binary string
	Run    func(context.Context, string, ...string) error
}

func (verifier CommandBlobSignatureVerifier) VerifyBlob(ctx context.Context, blob, bundle []byte, policy ReleaseTrustPolicy) error {
	if policy.OIDCIssuer != GitHubActionsOIDCIssuer || policy.WorkflowIdentity == "" {
		return contractError("release trust policy")
	}
	directory, err := os.MkdirTemp("", "accountable-cosign-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(directory) }()
	blobPath := filepath.Join(directory, "release.json")
	bundlePath := filepath.Join(directory, "release.sigstore.json")
	if err := os.WriteFile(blobPath, blob, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(bundlePath, bundle, 0o600); err != nil {
		return err
	}
	binary := verifier.Binary
	if binary == "" {
		binary = "cosign"
	}
	run := verifier.Run
	if run == nil {
		run = runCommand
	}
	if err := run(
		ctx,
		binary,
		"verify-blob",
		"--bundle", bundlePath,
		"--certificate-identity", policy.WorkflowIdentity,
		"--certificate-oidc-issuer", policy.OIDCIssuer,
		blobPath,
	); err != nil {
		return contractError("release signature")
	}
	return nil
}

func runCommand(ctx context.Context, name string, arguments ...string) error {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Stdout = nil
	command.Stderr = nil
	return command.Run()
}
