package telemetry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var excludedWebAssetPaths = map[string]struct{}{
	"_runtime/config.json":  {},
	"_runtime/release.json": {},
}

type WebFileEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

type WebFileManifest struct {
	SchemaVersion int            `json:"schema_version"`
	Files         []WebFileEntry `json:"files"`
}

type ProducedWebFileManifest struct {
	Manifest       WebFileManifest
	CanonicalJSON  []byte
	WebAssetDigest string
}

type WebRuntimeRelease struct {
	SchemaVersion         int    `json:"schema_version"`
	ReleaseID             string `json:"release_id"`
	ReleaseManifestDigest string `json:"release_manifest_digest"`
	WebAssetDigest        string `json:"web_asset_digest"`
}

func BuildWebFileManifest(root string) (ProducedWebFileManifest, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return ProducedWebFileManifest{}, contractError("web root")
	}
	manifest := WebFileManifest{SchemaVersion: ContractSchemaVersion}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return contractError("web asset symlink")
		}
		if entry.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return contractError("web asset type")
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if !validRelativeAssetPath(relative) {
			return contractError("web asset path")
		}
		if _, excluded := excludedWebAssetPaths[relative]; excluded {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		manifest.Files = append(manifest.Files, WebFileEntry{
			Path:   relative,
			Size:   int64(len(raw)),
			Digest: "sha256:" + sha256Hex(raw),
		})
		return nil
	})
	if err != nil {
		return ProducedWebFileManifest{}, err
	}
	sort.Slice(manifest.Files, func(left, right int) bool {
		return manifest.Files[left].Path < manifest.Files[right].Path
	})
	raw, err := json.Marshal(manifest)
	if err != nil {
		return ProducedWebFileManifest{}, err
	}
	canonical, err := CanonicalizeJSON(raw)
	if err != nil {
		return ProducedWebFileManifest{}, err
	}
	return ProducedWebFileManifest{
		Manifest:       manifest,
		CanonicalJSON:  canonical,
		WebAssetDigest: "sha256:" + sha256Hex(canonical),
	}, nil
}

func VerifyWebFileManifest(root string, expected []byte, expectedDigest string) error {
	produced, err := BuildWebFileManifest(root)
	if err != nil {
		return err
	}
	canonicalExpected, err := CanonicalizeJSON(expected)
	if err != nil || !bytes.Equal(canonicalExpected, produced.CanonicalJSON) || expectedDigest != produced.WebAssetDigest {
		return contractError("web file manifest")
	}
	return nil
}

func BuildWebRuntimeRelease(releaseID, releaseManifestDigest, webAssetDigest string) ([]byte, error) {
	if !uuidV7Pattern.MatchString(releaseID) || !digestPattern.MatchString(releaseManifestDigest) || !digestPattern.MatchString(webAssetDigest) {
		return nil, contractError("web runtime release")
	}
	return json.Marshal(WebRuntimeRelease{
		SchemaVersion:         ContractSchemaVersion,
		ReleaseID:             releaseID,
		ReleaseManifestDigest: releaseManifestDigest,
		WebAssetDigest:        webAssetDigest,
	})
}

func validRelativeAssetPath(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") || strings.ContainsRune(path, 0) {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	return clean == path && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func ParseWebFileManifest(raw []byte) (WebFileManifest, error) {
	var manifest WebFileManifest
	if err := decodeStrictJSON(raw, &manifest); err != nil {
		return WebFileManifest{}, err
	}
	if manifest.SchemaVersion != ContractSchemaVersion {
		return WebFileManifest{}, contractError("web file manifest schema")
	}
	previousPath := ""
	for _, file := range manifest.Files {
		if !validRelativeAssetPath(file.Path) || file.Path <= previousPath || file.Size < 0 || !digestPattern.MatchString(file.Digest) {
			return WebFileManifest{}, fmt.Errorf("%w: web file entry", ErrInvalidContract)
		}
		if _, excluded := excludedWebAssetPaths[file.Path]; excluded {
			return WebFileManifest{}, contractError("excluded web runtime file")
		}
		previousPath = file.Path
	}
	return manifest, nil
}
