package telemetry

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	maximumWebArchiveFileBytes = int64(512 << 20)
	maximumWebArchiveBytes     = int64(1 << 30)
)

func ExtractWebArchive(archivePath, root string) error {
	if !filepath.IsAbs(root) {
		return contractError("web archive root")
	}
	if err := prepareEmptyArchiveRoot(root); err != nil {
		return err
	}
	archive, err := os.Open(filepath.Clean(archivePath))
	if err != nil {
		return err
	}
	defer func() { _ = archive.Close() }()
	compressed, err := gzip.NewReader(archive)
	if err != nil {
		return contractError("web archive compression")
	}
	defer func() { _ = compressed.Close() }()

	reader := tar.NewReader(compressed)
	seen := make(map[string]struct{})
	var extractedBytes int64
	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return contractError("web archive")
		}
		name, valid := validWebArchivePath(header.Name)
		if !valid {
			return contractError("web archive path")
		}
		if _, duplicate := seen[name]; duplicate {
			return contractError("web archive duplicate")
		}
		seen[name] = struct{}{}
		target := filepath.Join(root, filepath.FromSlash(name))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case 0, tar.TypeReg:
			if header.Size < 0 || header.Size > maximumWebArchiveFileBytes || extractedBytes > maximumWebArchiveBytes-header.Size {
				return contractError("web archive size")
			}
			extractedBytes += header.Size
			if err := writeArchiveFile(reader, target, header.Size); err != nil {
				return err
			}
		default:
			return contractError("web archive entry type")
		}
	}
	return nil
}

func prepareEmptyArchiveRoot(root string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return contractError("web archive root")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return contractError("web archive root")
	}
	return nil
}

func validWebArchivePath(value string) (string, bool) {
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || strings.ContainsRune(value, 0) {
		return "", false
	}
	trimmed := strings.TrimSuffix(value, "/")
	clean := path.Clean(trimmed)
	return clean, clean == trimmed && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func writeArchiveFile(reader io.Reader, target string, expectedBytes int64) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != expectedBytes {
		return contractError("web archive entry size")
	}
	return nil
}
