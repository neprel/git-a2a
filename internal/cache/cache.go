package cache

import (
	"fmt"
	"os"
	"path/filepath"
)

func Dir(root, id string) string { return filepath.Join(root, ".git-a2a", "cache", id) }

func Save(root, id string, manifest []byte, commit, method string) error {
	dir := Dir(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "a2amodule.yml"), manifest, 0o644); err != nil {
		return err
	}
	meta := []byte(fmt.Sprintf("commit: %s\nmethod: %s\n", commit, method))
	return os.WriteFile(filepath.Join(dir, "meta.yml"), meta, 0o644)
}
