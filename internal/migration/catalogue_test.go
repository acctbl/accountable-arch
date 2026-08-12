package migration

import (
	"bytes"
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestCommittedCatalogueMatchesFilenameContract(t *testing.T) {
	t.Parallel()

	if _, err := mergeSources(Catalogue()); err != nil {
		t.Fatalf("committed migration catalogue: %v", err)
	}
}

func TestCommittedCatalogueDoesNotDependOnManagedDatabaseRoles(t *testing.T) {
	t.Parallel()

	migrations, err := mergeSources(Catalogue())
	if err != nil {
		t.Fatal(err)
	}
	err = fs.WalkDir(migrations, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		payload, readErr := fs.ReadFile(migrations, path)
		if readErr != nil {
			return readErr
		}
		for _, role := range [][]byte{[]byte("accountable_admin"), []byte("accountable_api"), []byte("accountable_migrate")} {
			if bytes.Contains(payload, role) {
				t.Errorf("migration %s depends on managed database role %s", path, role)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMergeSourcesRejectsBadNamesAndDuplicateVersions(t *testing.T) {
	t.Parallel()

	valid := []byte("-- +goose Up\nSELECT 1;\n")
	for name, sources := range map[string][]Source{
		"bad filename": {{Owner: "bad", FS: fs.FS(fstest.MapFS{"00001_bad.sql": &fstest.MapFile{Data: valid}})}},
		"duplicate version": {
			{Owner: "first", FS: fs.FS(fstest.MapFS{"20260801000000_first_init.sql": &fstest.MapFile{Data: valid}})},
			{Owner: "second", FS: fs.FS(fstest.MapFS{"20260801000000_second_init.sql": &fstest.MapFile{Data: valid}})},
		},
	} {
		if _, err := mergeSources(sources); err == nil {
			t.Errorf("%s: mergeSources accepted invalid catalogue", name)
		}
	}
}
