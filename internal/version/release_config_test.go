package version

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func TestReleaseChannelManifestsUseImmutableChecksums(t *testing.T) {
	root := repositoryRoot(t)
	temp := t.TempDir()
	checksums := filepath.Join(temp, "checksums.txt")
	formula := filepath.Join(temp, "Formula", "git-a2a.rb")
	scoop := filepath.Join(temp, "git-a2a.json")
	body := strings.Join([]string{strings.Repeat("a", 64) + "  git-a2a_brew_1.2.3_darwin_amd64.tar.gz", strings.Repeat("b", 64) + "  git-a2a_brew_1.2.3_darwin_arm64.tar.gz", strings.Repeat("c", 64) + "  git-a2a_scoop_1.2.3_windows_amd64.zip", strings.Repeat("d", 64) + "  git-a2a_scoop_1.2.3_windows_arm64.zip"}, "\n") + "\n"
	if err := os.WriteFile(checksums, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("python3", filepath.Join(root, "tools/release-channels.py"), "--tag", "v1.2.3", "--checksums", checksums, "--homebrew", formula, "--scoop", scoop)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("render: %v: %s", err, out)
	}
	formulaBody, err := os.ReadFile(formula)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64), "git-a2a_brew_1.2.3"} {
		if !strings.Contains(string(formulaBody), want) {
			t.Errorf("formula missing %q", want)
		}
	}
	var manifest struct {
		Architecture map[string]struct {
			Hash string `json:"hash"`
		} `json:"architecture"`
	}
	b, err := os.ReadFile(scoop)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Architecture["64bit"].Hash != strings.Repeat("c", 64) || manifest.Architecture["arm64"].Hash != strings.Repeat("d", 64) {
		t.Fatal("Scoop hashes differ")
	}
}

func TestReleaseConfigurationPreservesBinaryChannelsWithoutRemovedSubsystems(t *testing.T) {
	root := repositoryRoot(t)
	read := func(path string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	workflow := read(".github/workflows/release.yml")
	ci := read(".github/workflows/ci.yml")
	smoke := read(".github/workflows/release-smoke.yml")
	goreleaser := read(".goreleaser.yaml")
	for _, want := range []string{"contents: write", "packages: write", "id-token: write", "attestations: write", "npm publish", "pypi", "tools/release-channels.py", "cosign sign --yes"} {
		if !strings.Contains(workflow, want) {
			t.Errorf("release workflow missing %q", want)
		}
	}
	for _, want := range []string{"darwin", "linux", "windows", "amd64", "arm64", "go test -count=1 ./..."} {
		if !strings.Contains(ci, want) {
			t.Errorf("CI missing %q", want)
		}
	}
	for _, want := range []string{"ubuntu-latest", "macos-latest", "windows-latest", "--version", " init", " list"} {
		if !strings.Contains(smoke, want) {
			t.Errorf("smoke missing %q", want)
		}
	}
	for _, want := range []string{"release:", "prerelease: auto", "go mod verify", "org.opencontainers.image.source"} {
		if !strings.Contains(goreleaser, want) {
			t.Errorf("GoReleaser missing %q", want)
		}
	}
	combined := strings.ToLower(workflow + ci + smoke + goreleaser + read("dist/npm/package.json"))
	for _, forbidden := range []string{"mcpb", "mcp-publisher", "modelcontextprotocol", "mcpname", "setup --", "git-a2a upgrade"} {
		if strings.Contains(combined, forbidden) {
			t.Errorf("removed release feature remains: %s", forbidden)
		}
	}
	if out, err := exec.Command("sh", "-n", filepath.Join(root, "install.sh")).CombinedOutput(); err != nil {
		t.Fatalf("install.sh: %v: %s", err, out)
	}
}
