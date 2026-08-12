package secret

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maximumSecretBytes = 64 << 10

type SecretSource interface {
	ResolveBatch(context.Context, []Ref) (map[Ref]SecretValue, error)
}

type OptionalSecretSource interface {
	SecretSource
	ResolveOptional(context.Context, Ref) (SecretValue, bool, error)
}

type SecretValue struct{ bytes []byte }

func NewSecretValue(value []byte) SecretValue {
	return SecretValue{bytes: bytes.Clone(value)}
}

func (v SecretValue) Bytes() []byte { return bytes.Clone(v.bytes) }

func (SecretValue) String() string   { return "[REDACTED]" }
func (SecretValue) GoString() string { return "[REDACTED]" }

type FileSecretSource struct{ root string }

func NewFileSecretSource(root string) (*FileSecretSource, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("secret directory must be absolute")
	}
	resolvedRoot, err := filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return nil, errors.New("secret directory is unavailable")
	}
	info, err := os.Lstat(resolvedRoot)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("secret directory is unavailable")
	}
	return &FileSecretSource{root: resolvedRoot}, nil
}

func (r *FileSecretSource) ResolveBatch(ctx context.Context, refs []Ref) (map[Ref]SecretValue, error) {
	values := make(map[Ref]SecretValue, len(refs))
	for _, ref := range refs {
		if _, duplicate := values[ref]; duplicate {
			return nil, errors.New("secret references must be unique")
		}
		value, err := r.resolve(ctx, ref)
		if err != nil {
			return nil, err
		}
		values[ref] = value
	}
	return values, nil
}

func (r *FileSecretSource) ResolveOptional(ctx context.Context, ref Ref) (SecretValue, bool, error) {
	parsed, err := ParseRef(string(ref))
	if err != nil {
		return SecretValue{}, false, errors.New("secret reference is invalid")
	}
	path := filepath.Join(r.root, filepath.FromSlash(string(parsed)))
	if !within(r.root, path) {
		return SecretValue{}, false, errors.New("secret reference is invalid")
	}
	resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if os.IsNotExist(err) {
		return SecretValue{}, false, nil
	}
	if err != nil || !within(r.root, resolvedParent) {
		return SecretValue{}, false, errors.New("secret is unavailable")
	}
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return SecretValue{}, false, nil
	} else if err != nil {
		return SecretValue{}, false, errors.New("secret is unavailable")
	}
	value, err := r.resolve(ctx, parsed)
	if err != nil {
		return SecretValue{}, false, err
	}
	return value, true, nil
}

func (r *FileSecretSource) resolve(ctx context.Context, ref Ref) (SecretValue, error) {
	if err := ctx.Err(); err != nil {
		return SecretValue{}, err
	}
	parsed, err := ParseRef(string(ref))
	if err != nil {
		return SecretValue{}, errors.New("secret reference is invalid")
	}
	path := filepath.Join(r.root, filepath.FromSlash(string(parsed)))
	if !within(r.root, path) {
		return SecretValue{}, errors.New("secret reference is invalid")
	}
	resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil || !within(r.root, resolvedParent) {
		return SecretValue{}, errors.New("secret is unavailable")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return SecretValue{}, errors.New("secret is unavailable")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return SecretValue{}, errors.New("secret file permissions are unsafe")
	}
	file, err := os.Open(path)
	if err != nil {
		return SecretValue{}, errors.New("secret is unavailable")
	}
	defer func() { _ = file.Close() }()
	value, err := io.ReadAll(io.LimitReader(file, maximumSecretBytes+1))
	if err != nil || len(value) > maximumSecretBytes {
		return SecretValue{}, errors.New("secret cannot be read safely")
	}
	value = bytes.TrimSuffix(value, []byte{'\n'})
	value = bytes.TrimSuffix(value, []byte{'\r'})
	if len(value) == 0 {
		return SecretValue{}, errors.New("secret is empty")
	}
	return NewSecretValue(value), nil
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !filepath.IsAbs(relative) &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
