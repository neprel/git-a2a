package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathAcceptsEitherManifestExtensionAndRejectsBoth(t *testing.T) {
	for _, name := range []string{CanonicalName, AlternateName} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, name), []byte("schema: 1\nmodule: {id: acme-lib}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := Path(dir)
			if err != nil || filepath.Base(got) != name {
				t.Fatalf("Path() = %q, %v", got, err)
			}
			if _, err := LoadDir(dir); err != nil {
				t.Fatal(err)
			}
		})
	}
	dir := t.TempDir()
	for _, name := range []string{CanonicalName, AlternateName} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("schema: 1\nmodule: {id: acme-lib}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Path(dir); err == nil || !strings.Contains(err.Error(), "exactly one of") {
		t.Fatalf("Path() error = %v", err)
	}
}
