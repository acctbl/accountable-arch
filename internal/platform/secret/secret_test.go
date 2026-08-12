package secret

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSecretSourceReturnsRedactedValue(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	secret := []byte("postgres://accountable:super-secret@localhost/accountable\n")
	if err := os.WriteFile(filepath.Join(directory, "database.api_dsn"), secret, 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	resolver, err := NewFileSecretSource(directory)
	if err != nil {
		t.Fatalf("NewFileSecretSource: %v", err)
	}
	values, err := resolver.ResolveBatch(context.Background(), []Ref{"database.api_dsn"})
	if err != nil {
		t.Fatalf("ResolveBatch: %v", err)
	}
	value := values["database.api_dsn"]
	if got := string(value.Bytes()); got != strings.TrimSpace(string(secret)) {
		t.Fatalf("resolved value = %q", got)
	}
	if got := value.String(); got != "[REDACTED]" {
		t.Fatalf("String = %q", got)
	}
}

func TestFileSecretSourceRefusesMissingUnsafeOrEscapingFiles(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "crypto.key")
	if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	resolver, err := NewFileSecretSource(directory)
	if err != nil {
		t.Fatalf("NewFileSecretSource: %v", err)
	}
	if _, err := resolver.ResolveBatch(context.Background(), []Ref{"crypto.key"}); err == nil {
		t.Fatal("Resolve succeeded with group/world-readable secret")
	}
	if _, err := resolver.ResolveBatch(context.Background(), []Ref{"missing.key"}); err == nil {
		t.Fatal("Resolve succeeded with missing secret")
	}

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "escaped.key"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("write outside secret: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := resolver.ResolveBatch(context.Background(), []Ref{"escape/escaped.key"}); err == nil {
		t.Fatal("Resolve followed an intermediate symlink outside the secret directory")
	}
}

func TestFileSecretSourceResolvesBatchAtomically(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "one"), []byte("first"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	resolver, err := NewFileSecretSource(directory)
	if err != nil {
		t.Fatalf("NewFileSecretSource: %v", err)
	}
	if values, err := resolver.ResolveBatch(context.Background(), []Ref{"one", "missing"}); err == nil || values != nil {
		t.Fatalf("partial batch = (%v, %v), want all-or-error", values, err)
	}
	if values, err := resolver.ResolveBatch(context.Background(), []Ref{"one", "one"}); err == nil || values != nil {
		t.Fatalf("duplicate batch = (%v, %v), want refusal", values, err)
	}
}

func TestFileSecretSourceTreatsOnlyAMissingOptionalSecretAsAbsent(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "standby"), []byte("standby-secret"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	resolver, err := NewFileSecretSource(directory)
	if err != nil {
		t.Fatal(err)
	}
	value, present, err := resolver.ResolveOptional(context.Background(), "standby")
	if err != nil || !present || string(value.Bytes()) != "standby-secret" {
		t.Fatalf("present optional secret = %q, %t, %v", value.Bytes(), present, err)
	}
	_, present, err = resolver.ResolveOptional(context.Background(), "missing")
	if err != nil || present {
		t.Fatalf("missing optional secret = %t, %v", present, err)
	}
}
