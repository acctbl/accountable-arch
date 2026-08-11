package configfile

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

func AbsolutePath(args []string) (string, error) {
	paths, err := AbsolutePaths(args, "--config")
	if err != nil {
		return "", err
	}
	return paths["--config"], nil
}

// AbsolutePaths reports one absolute path for each required flag. The argument
// list must carry exactly those flags, so an unexpected argument never passes.
func AbsolutePaths(args []string, flags ...string) (map[string]string, error) {
	usage := fmt.Errorf("usage: %s", strings.Join(flagUsage(flags), " "))
	if len(args) != len(flags)*2 {
		return nil, usage
	}
	seen := make(map[string]string, len(flags))
	for index := 0; index < len(args); index += 2 {
		name, value := args[index], args[index+1]
		if _, duplicate := seen[name]; duplicate || value == "" || !filepath.IsAbs(value) {
			return nil, usage
		}
		seen[name] = filepath.Clean(value)
	}
	for _, flag := range flags {
		if _, present := seen[flag]; !present {
			return nil, usage
		}
	}
	return seen, nil
}

func flagUsage(flags []string) []string {
	usage := make([]string, 0, len(flags))
	for _, flag := range flags {
		usage = append(usage, flag+" <absolute-path>")
	}
	return usage
}

func Decode(path string, target any) error {
	_, err := DecodeWithFingerprint(path, target)
	return err
}

func DecodeWithFingerprint(path string, target any) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("open config: %w", err)
	}

	decoder := toml.NewDecoder(bytes.NewReader(contents)).DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return "", fmt.Errorf("decode config: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(contents)), nil
}
