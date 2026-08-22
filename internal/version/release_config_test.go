package version

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseConfigurationKeepsDistributionGates(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	read := func(path string) string {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	workflow := read(".github/workflows/release.yml")
	for _, required := range []string{"needs: test", "permissions: {}", "contents: write", "packages: write", "id-token: write", "--skip=homebrew", "--skip=scoop", "NPM_TOKEN == ''"} {
		if !strings.Contains(workflow, required) {
			t.Errorf("release workflow missing %q", required)
		}
	}
	allWorkflows := workflow + "\n" + read(".github/workflows/ci.yml")
	action := regexp.MustCompile(`(?m)^\s*- uses: [^@]+@[0-9a-f]{40}(?:\s+# v[^\s]+)$`)
	uses := regexp.MustCompile(`(?m)^\s*- uses:`).FindAllStringIndex(allWorkflows, -1)
	if matches := action.FindAllStringIndex(allWorkflows, -1); len(matches) != len(uses) {
		t.Errorf("every pinned action needs a version comment: uses=%d commented=%d", len(uses), len(matches))
	}
	if config := read(".goreleaser.yaml"); !strings.Contains(config, "prerelease: auto") || !strings.Contains(config, "go mod verify") {
		t.Error("GoReleaser prerelease or clean-tree gate is missing")
	}
	installer := read("install.sh")
	for _, required := range []string{"sha256sum", "url_effective", "mingw", "not writable", "version=\"v$version\""} {
		if !strings.Contains(strings.ToLower(installer), strings.ToLower(required)) {
			t.Errorf("installer missing %q", required)
		}
	}
	if docs := read("docs/releasing.md"); !strings.Contains(docs, "HOMEBREW_TAP_TOKEN") || !strings.Contains(docs, "SCOOP_BUCKET_TOKEN") || !strings.Contains(docs, "NPM_TOKEN") || !strings.Contains(docs, "environment named `pypi`") {
		t.Error("release prerequisites are incomplete")
	}
	command := exec.Command("sh", "-n", filepath.Join(root, "install.sh"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("install.sh syntax: %v: %s", err, output)
	}
}
