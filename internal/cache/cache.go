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
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, "."+filepath.Base(dir)+"-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err = os.WriteFile(filepath.Join(stage, name), body, 0o644); err != nil {
		return err
	}
	meta := []byte(fmt.Sprintf("commit: %s\nmethod: %s\n", commit, method))
	if err = os.WriteFile(filepath.Join(stage, "meta.yml"), meta, 0o644); err != nil {
		return err
	}
	backup := ""
	if _, statErr := os.Stat(dir); statErr == nil {
		backup, err = os.MkdirTemp(parent, "."+filepath.Base(dir)+"-backup-")
		if err != nil {
			return err
		}
		if err = os.Remove(backup); err != nil {
			return err
		}
		if err = os.Rename(dir, backup); err != nil {
			return err
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	if err = os.Rename(stage, dir); err != nil {
		if backup != "" {
			if restoreErr := os.Rename(backup, dir); restoreErr != nil {
				return fmt.Errorf("cache replace failed: %v; restoring previous cache failed: %w", err, restoreErr)
			}
		}
		return err
	}
	if backup != "" {
		_ = os.RemoveAll(backup)
	}
	return nil
}
