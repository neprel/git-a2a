package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAsReplacesTheWholeCacheEntry(t *testing.T) {
	root := t.TempDir()
	if err := SaveAs(root, "lib", []byte("old\n"), "old", "archive", "a2amodule.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(Dir(root, "lib"), "stale"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveAs(root, "lib", []byte("new\n"), "new", "sparse", "a2amodule.yml"); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(Dir(root, "lib"), "a2amodule.yml")); err != nil || string(body) != "new\n" {
		t.Fatalf("manifest = %q, err=%v", body, err)
	}
	for _, absent := range []string{"a2amodule.yaml", "stale"} {
		if _, err := os.Stat(filepath.Join(Dir(root, "lib"), absent)); !os.IsNotExist(err) {
			t.Fatalf("%s survived replacement: %v", absent, err)
		}
	}
}
