package telemetry

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractWebArchiveAcceptsOnlyBoundedRegularFiles(t *testing.T) {
	archive := writeWebArchive(t, []tar.Header{
		{Name: "assets/", Typeflag: tar.TypeDir},
		{Name: "assets/app.js", Typeflag: tar.TypeReg, Size: 3},
	}, [][]byte{nil, []byte("app")})
	root := filepath.Join(t.TempDir(), "dist")
	if err := ExtractWebArchive(archive, root); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "assets", "app.js"))
	if err != nil || string(raw) != "app" {
		t.Fatalf("extracted file = %q, error = %v", raw, err)
	}
}

func TestExtractWebArchiveAcceptsLegacyRegularFileType(t *testing.T) {
	archive := writeWebArchive(t, []tar.Header{
		{Name: "index.html", Typeflag: 0, Size: 2},
	}, [][]byte{[]byte("ok")})
	root := filepath.Join(t.TempDir(), "dist")
	if err := ExtractWebArchive(archive, root); err != nil {
		t.Fatal(err)
	}
}

func TestExtractWebArchiveRejectsUnsafeEntries(t *testing.T) {
	for name, header := range map[string]tar.Header{
		"traversal": {Name: "../escape", Typeflag: tar.TypeReg},
		"absolute":  {Name: "/escape", Typeflag: tar.TypeReg},
		"symlink":   {Name: "escape", Typeflag: tar.TypeSymlink, Linkname: "../escape"},
	} {
		t.Run(name, func(t *testing.T) {
			archive := writeWebArchive(t, []tar.Header{header}, [][]byte{nil})
			err := ExtractWebArchive(archive, filepath.Join(t.TempDir(), "dist"))
			if !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func writeWebArchive(t *testing.T, headers []tar.Header, bodies [][]byte) string {
	t.Helper()
	archivePath := filepath.Join(t.TempDir(), "web.tar.gz")
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzip.NewWriter(archive)
	writer := tar.NewWriter(compressed)
	for index := range headers {
		if err := writer.WriteHeader(&headers[index]); err != nil {
			t.Fatal(err)
		}
		if len(bodies[index]) > 0 {
			if _, err := writer.Write(bodies[index]); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return archivePath
}
