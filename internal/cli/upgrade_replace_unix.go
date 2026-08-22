//go:build !windows

package cli

import "os"

func cleanupPreviousUpgrade() {}

func replaceExecutable(executable, replacement string) error {
	return os.Rename(replacement, executable)
}
