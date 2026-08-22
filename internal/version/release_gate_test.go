package version

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseVersionGateRejectsMismatchedTag(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	script := filepath.Join(root, ".github", "scripts", "check-version.sh")

	matching := exec.Command(script, "v"+Current())
	matching.Dir = root
	if output, err := matching.CombinedOutput(); err != nil {
		t.Fatalf("matching tag failed: %v: %s", err, output)
	}
	prerelease := exec.Command(script, "v"+Current()+"-rc.1")
	prerelease.Dir = root
	if output, err := prerelease.CombinedOutput(); err != nil {
		t.Fatalf("matching prerelease failed: %v: %s", err, output)
	}

	mismatch := exec.Command(script, "v999.0.0")
	mismatch.Dir = root
	output, err := mismatch.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "does not match") {
		t.Fatalf("mismatched tag: err=%v output=%q", err, output)
	}
}
