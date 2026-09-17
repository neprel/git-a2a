package composer

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestPublicCLIComposerNativeLifecycle is intentionally gated. The pinned
// runner supplies a Linux CLI through GITA2A_CLI_BIN and advertises Composer as
// mandatory, so a missing or broken native tool fails instead of silently
// turning into a successful skip.
func TestPublicCLIComposerNativeLifecycle(t *testing.T) {
	if os.Getenv("GITA2A_NATIVE_COMPOSER") != "1" {
		t.Skip("run the composer case through tools/native-lifecycle/run.py")
	}
	cli := os.Getenv("GITA2A_CLI_BIN")
	if cli == "" {
		t.Fatal("GITA2A_CLI_BIN must name the public CLI built for the native runner")
	}
	if _, err := os.Stat(cli); err != nil {
		t.Fatalf("GITA2A_CLI_BIN %q: %v", cli, err)
	}
	for _, tool := range []string{"composer", "git", "php"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("required native tool %s: %v", tool, err)
		}
	}

	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate Composer native test")
	}
	script := filepath.Join(filepath.Dir(here), "native", "lifecycle.sh")
	if override := os.Getenv("GITA2A_COMPOSER_LIFECYCLE"); override != "" {
		script = override
	}
	// The Docker image carries the script at a stable location because the test
	// binary intentionally runs without a source-tree mount.
	if _, err := os.Stat(script); err != nil {
		script = "/opt/git-a2a-composer-lifecycle.sh"
	}
	cmd := exec.Command(script)
	cmd.Env = append(os.Environ(), "GITA2A_CLI_BIN="+cli)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("Composer public-CLI lifecycle: %v", err)
	}
}
