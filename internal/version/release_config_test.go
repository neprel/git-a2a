package version

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	for _, want := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64), "git-a2a_brew_1.2.3", `license "MIT"`} {
		if !strings.Contains(string(formulaBody), want) {
			t.Errorf("formula missing %q", want)
		}
	}
	if strings.Contains(string(formulaBody), "Apache-2.0") {
		t.Fatal("formula license differs from the project MIT license")
	}
	license, err := os.ReadFile(filepath.Join(root, "LICENSE"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(license), "MIT License") || !strings.Contains(string(formulaBody), `license "MIT"`) {
		t.Fatal("Homebrew license must follow the project LICENSE")
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

func TestReleaseRecoveryPolicyDoesNotDowngradeChannels(t *testing.T) {
	root := repositoryRoot(t)
	script := filepath.Join(root, "tools/release-policy.py")
	tests := []struct {
		name              string
		candidate         string
		stable            string
		prerelease        string
		exists            bool
		publishRelease    bool
		preserve          bool
		promoteStable     bool
		promotePrerelease bool
	}{
		{"older recovery after newer stable", "v2.0.0", "v2.1.0", "v2.2.0-rc.1", true, false, true, false, false},
		{"repeat current stable", "v2.1.0", "v2.1.0", "v2.2.0-rc.1", true, false, true, true, false},
		{"partially published current stable", "v2.1.0", "v2.0.0", "", true, false, true, true, false},
		{"prerelease leaves stable channels", "v2.2.0-rc.2", "v2.1.0", "v2.2.0-rc.1", false, true, false, false, true},
		{"older prerelease does not move next", "v2.2.0-rc.1", "v2.1.0", "v2.2.0-rc.2", true, false, true, false, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := exec.Command("python3", script, "plan",
				"--candidate", test.candidate,
				"--latest-stable", test.stable,
				"--latest-prerelease", test.prerelease,
				"--release-exists", map[bool]string{true: "true", false: "false"}[test.exists],
			)
			output, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				PublishRelease    bool `json:"publish_release"`
				PreserveImmutable bool `json:"preserve_immutable"`
				PromoteStable     bool `json:"promote_stable"`
				PromotePrerelease bool `json:"promote_prerelease"`
			}
			if err := json.Unmarshal(output, &got); err != nil {
				t.Fatal(err)
			}
			if got.PublishRelease != test.publishRelease || got.PreserveImmutable != test.preserve || got.PromoteStable != test.promoteStable || got.PromotePrerelease != test.promotePrerelease {
				t.Fatalf("unexpected plan: %+v", got)
			}
		})
	}
}

func TestReleaseRecoveryPolicySelectsSemanticNewestVersions(t *testing.T) {
	root := repositoryRoot(t)
	script := filepath.Join(root, "tools/release-policy.py")
	releases := `[
		{"tagName":"v2.9.0","isDraft":false,"isPrerelease":false},
		{"tagName":"v2.10.0","isDraft":false,"isPrerelease":false},
		{"tagName":"v3.0.0-rc.2","isDraft":false,"isPrerelease":true},
		{"tagName":"v3.0.0-rc.10","isDraft":false,"isPrerelease":true},
		{"tagName":"v9.0.0","isDraft":true,"isPrerelease":false}
	]`
	for _, test := range []struct {
		kind string
		want string
	}{{"stable", "v2.10.0"}, {"prerelease", "v3.0.0-rc.10"}} {
		cmd := exec.Command("python3", script, "latest", "--kind", test.kind)
		cmd.Stdin = strings.NewReader(releases)
		output, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(string(output)); got != test.want {
			t.Fatalf("latest %s = %q, want %q", test.kind, got, test.want)
		}
	}
}

func releasePromotion(t *testing.T, candidate, stable string) bool {
	t.Helper()
	cmd := exec.Command("python3", filepath.Join(repositoryRoot(t), "tools/release-policy.py"), "plan",
		"--candidate", candidate,
		"--latest-stable", stable,
		"--release-exists", "true",
	)
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var plan struct {
		PromoteStable bool `json:"promote_stable"`
	}
	if err := json.Unmarshal(output, &plan); err != nil {
		t.Fatal(err)
	}
	return plan.PromoteStable
}

func TestReleasePublicationLockPreventsStalePromotion(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(workflow)
	for _, want := range []string{"concurrency:\n  group: release-publication\n  cancel-in-progress: false", "needs.policy.outputs.promote_stable", "needs.policy.outputs.promote_prerelease"} {
		if !strings.Contains(body, want) {
			t.Fatalf("release workflow missing global publication safeguard %q", want)
		}
	}
	if strings.Count(body, "needs.policy.outputs.promote_stable") < 3 {
		t.Fatal("GHCR latest, Homebrew/Scoop, and npm latest must all use the locked stable policy")
	}
	if !strings.Contains(body, "PROMOTE_PRERELEASE: ${{ needs.policy.outputs.promote_prerelease }}") {
		t.Fatal("npm next must use the locked prerelease policy")
	}

	for _, order := range [][]string{{"v2.0.0", "v2.1.0"}, {"v2.1.0", "v2.0.0"}} {
		latestRelease := "v2.0.0"
		stableChannel := "v2.0.0"
		for _, candidate := range order {
			promote := releasePromotion(t, candidate, latestRelease)
			if promote {
				stableChannel = candidate
			}
			if promote && candidate == "v2.1.0" {
				latestRelease = candidate
			}
		}
		if stableChannel != "v2.1.0" {
			t.Fatalf("serialized order %v ended at %s", order, stableChannel)
		}
	}
}

func TestPyPIRecoverySelectsMissingWheels(t *testing.T) {
	const version = "2.1.0"
	platforms := []string{"macosx_10_15_x86_64", "macosx_11_0_arm64", "manylinux_2_17_x86_64", "manylinux_2_17_aarch64", "win_amd64", "win_arm64"}
	type wheel struct {
		filename string
		digest   string
	}
	wheels := make([]wheel, 0, len(platforms))
	wheelhouse := t.TempDir()
	for _, platform := range platforms {
		filename := fmt.Sprintf("git_a2a-%s-py3-none-%s.whl", version, platform)
		content := []byte("wheel:" + platform)
		if err := os.WriteFile(filepath.Join(wheelhouse, filename), content, 0o600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(content)
		wheels = append(wheels, wheel{filename: filename, digest: fmt.Sprintf("%x", digest)})
	}

	metadata := func(published []wheel) []byte {
		urls := make([]map[string]any, 0, len(published))
		for _, item := range published {
			urls = append(urls, map[string]any{"filename": item.filename, "digests": map[string]string{"sha256": item.digest}})
		}
		body, err := json.Marshal(map[string]any{"urls": urls})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}

	tests := []struct {
		name        string
		status      int
		published   []wheel
		wantMissing int
		wantError   string
		unavailable bool
	}{
		{name: "version absent", status: http.StatusNotFound, wantMissing: 6},
		{name: "one wheel published", status: http.StatusOK, published: wheels[:1], wantMissing: 5},
		{name: "all wheels published", status: http.StatusOK, published: wheels, wantMissing: 0},
		{name: "checksum conflict", status: http.StatusOK, published: []wheel{{filename: wheels[0].filename, digest: strings.Repeat("0", 64)}}, wantError: "checksum conflict"},
		{name: "registry unavailable", wantError: "registry request failed", unavailable: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(test.status)
				if test.status == http.StatusOK {
					_, _ = response.Write(metadata(test.published))
				}
			}))
			if test.unavailable {
				server.Close()
			} else {
				defer server.Close()
			}
			output := t.TempDir()
			cmd := exec.Command("python3", filepath.Join(repositoryRoot(t), "tools/pypi-recovery.py"),
				"--wheelhouse", wheelhouse,
				"--out", output,
				"--version", version,
				"--metadata-url", server.URL,
			)
			combined, err := cmd.CombinedOutput()
			if test.wantError != "" {
				if err == nil || !strings.Contains(string(combined), test.wantError) {
					t.Fatalf("expected %q, got err=%v output=%s", test.wantError, err, combined)
				}
				if entries, readErr := os.ReadDir(output); readErr != nil || len(entries) != 0 {
					t.Fatalf("failed recovery populated upload directory: entries=%v err=%v", entries, readErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("select missing wheels: %v: %s", err, combined)
			}
			entries, err := os.ReadDir(output)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != test.wantMissing {
				t.Fatalf("missing wheel count = %d, want %d; output=%s", len(entries), test.wantMissing, combined)
			}
			published := make(map[string]bool, len(test.published))
			for _, item := range test.published {
				published[item.filename] = true
			}
			for _, entry := range entries {
				if published[entry.Name()] {
					t.Fatalf("already published wheel was selected for upload: %s", entry.Name())
				}
			}
		})
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
	for _, want := range []string{"contents: write", "packages: write", "id-token: write", "attestations: write", "npm publish", "pypi", "tools/release-channels.py", "tools/release-policy.py", "tools/pypi-recovery.py", "missing-wheelhouse", "cosign sign --yes", "preserve_immutable", "promote_stable", "temporary_tag", "node tools/npm-dist-tags.mjs", "node --test tools/npm-dist-tags.test.mjs", "docker manifest inspect"} {
		if !strings.Contains(workflow, want) {
			t.Errorf("release workflow missing %q", want)
		}
	}
	if strings.Contains(goreleaser, `{{ if not .Prerelease }}latest{{ end }}`) {
		t.Error("GoReleaser must not update GHCR latest outside downgrade policy")
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
