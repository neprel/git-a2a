package cache

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/neprel/git-a2a/internal/manifest"
)

func Dir(root, id string) string { return filepath.Join(root, ".git-a2a", "cache", id) }

func Save(root, id string, manifest []byte, commit, method string) error {
	return SaveAs(root, id, manifest, commit, method, "a2amodule.yml")
}

func SaveAs(root, id string, body []byte, commit, method, name string) error {
	if name != manifest.CanonicalName && name != manifest.AlternateName {
		return fmt.Errorf("cache: invalid manifest name %q", name)
	}
	dir := Dir(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(dir, manifest.CanonicalName))
	_ = os.Remove(filepath.Join(dir, manifest.AlternateName))
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
		return err
	}
	meta := []byte(fmt.Sprintf("commit: %s\nmethod: %s\n", commit, method))
	return os.WriteFile(filepath.Join(dir, "meta.yml"), meta, 0o644)
}
