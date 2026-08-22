//go:build windows

package cli

import (
	"fmt"
	"os"
	"path/filepath"
)

func cleanupPreviousUpgrade() {
	executable, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	_ = os.Remove(upgradeBackupPath(executable))
}

func replaceExecutable(executable, replacement string) error {
	backup := upgradeBackupPath(executable)
	_ = os.Remove(backup)
	if err := os.Rename(executable, backup); err != nil {
		return fmt.Errorf("move running executable aside: %w", err)
	}
	if err := os.Rename(replacement, executable); err != nil {
		if restoreErr := os.Rename(backup, executable); restoreErr != nil {
			return fmt.Errorf("install replacement: %w (restore failed: %v)", err, restoreErr)
		}
		return fmt.Errorf("install replacement: %w", err)
	}
	return nil
}
