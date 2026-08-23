package version

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestMCPDiscoveryBundleIsDeterministicAndHasRealHash(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	temp := t.TempDir()
	packages := filepath.Join(temp, "packages")
	for _, target := range []string{"darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64", "windows-amd64", "windows-arm64"} {
		executable := "git-a2a"
		if strings.HasPrefix(target, "windows-") {
			executable += ".exe"
		}
		path := filepath.Join(packages, target, "bin", executable)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("binary-"+target), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	build := func(out string) {
		t.Helper()
		command := exec.Command("python3", filepath.Join(root, "dist", "mcpb", "build.py"),
			"--packages", packages, "--version", "1.2.3", "--out", out)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build MCP discovery: %v: %s", err, output)
		}
	}
	first, second := filepath.Join(temp, "first"), filepath.Join(temp, "second")
	build(first)
	build(second)
	bundleName := "git-a2a_1.2.3.mcpb"
	firstBundle, err := os.ReadFile(filepath.Join(first, bundleName))
	if err != nil {
		t.Fatal(err)
	}
	secondBundle, _ := os.ReadFile(filepath.Join(second, bundleName))
	if !bytes.Equal(firstBundle, secondBundle) {
		t.Fatal("MCPB differs for identical inputs")
	}
	archive, err := zip.OpenReader(filepath.Join(first, bundleName))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.File) != 8 {
		t.Fatalf("MCPB entries = %d, want manifest + launcher + 6 binaries", len(archive.File))
	}
	for _, entry := range archive.File {
		if entry.Name == "server/launcher.js" && entry.Mode().Perm()&0o111 == 0 {
			t.Fatal("MCPB launcher is not executable")
		}
	}
	serverBody, err := os.ReadFile(filepath.Join(first, "server.json"))
	if err != nil {
		t.Fatal(err)
	}
	var server struct {
		Name     string `json:"name"`
		Packages []struct {
			RegistryType string `json:"registryType"`
			Identifier   string `json:"identifier"`
			FileSHA256   string `json:"fileSha256"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(serverBody, &server); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(firstBundle))
	if server.Name != "io.github.neprel/git-a2a" || len(server.Packages) != 3 || server.Packages[2].RegistryType != "mcpb" || server.Packages[2].FileSHA256 != digest || !strings.HasSuffix(server.Packages[2].Identifier, "/"+bundleName) {
		t.Fatalf("server.json discovery metadata = %#v", server)
	}
	secondServer, _ := os.ReadFile(filepath.Join(second, "server.json"))
	if !bytes.Equal(serverBody, secondServer) {
		t.Fatal("server.json differs for identical inputs")
	}
}

func TestReleaseChannelManifestsUseImmutableChecksums(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	temp := t.TempDir()
	checksums := filepath.Join(temp, "checksums.txt")
	formula := filepath.Join(temp, "Formula", "git-a2a.rb")
	scoop := filepath.Join(temp, "git-a2a.json")
	body := strings.Join([]string{
		strings.Repeat("a", 64) + "  git-a2a_brew_1.2.3_darwin_amd64.tar.gz",
		strings.Repeat("b", 64) + "  git-a2a_brew_1.2.3_darwin_arm64.tar.gz",
		strings.Repeat("c", 64) + "  git-a2a_scoop_1.2.3_windows_amd64.zip",
		strings.Repeat("d", 64) + "  git-a2a_scoop_1.2.3_windows_arm64.zip",
	}, "\n") + "\n"
	if err := os.WriteFile(checksums, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("python3", filepath.Join(root, "tools", "release-channels.py"),
		"--tag", "v1.2.3", "--checksums", checksums, "--homebrew", formula, "--scoop", scoop)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("render release channels: %v: %s", err, output)
	}
	formulaBody, err := os.ReadFile(formula)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64),
		`xattr -p com.apple.quarantine`, `xattr -d com.apple.quarantine`,
		`"$1" >/dev/null 2>&1`, "git-a2a_brew_1.2.3"} {
		if !strings.Contains(string(formulaBody), expected) {
			t.Errorf("Homebrew formula missing %q", expected)
		}
	}
	var scoopManifest struct {
		Architecture map[string]struct {
			Hash string `json:"hash"`
		} `json:"architecture"`
	}
	scoopBody, err := os.ReadFile(scoop)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(scoopBody, &scoopManifest); err != nil {
		t.Fatal(err)
	}
	if scoopManifest.Architecture["64bit"].Hash != strings.Repeat("c", 64) || scoopManifest.Architecture["arm64"].Hash != strings.Repeat("d", 64) {
		t.Fatal("Scoop manifest did not retain immutable release checksums")
	}
	missing := filepath.Join(temp, "missing-checksums.txt")
	if err := os.WriteFile(missing, []byte(strings.Repeat("a", 64)+"  git-a2a_brew_1.2.3_darwin_amd64.tar.gz\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command = exec.Command("python3", filepath.Join(root, "tools", "release-channels.py"),
		"--tag", "v1.2.3", "--checksums", missing, "--homebrew", formula, "--scoop", scoop)
	if output, err := command.CombinedOutput(); err == nil || !strings.Contains(string(output), "release checksum missing") {
		t.Fatalf("missing archive checksum must fail clearly: err=%v output=%s", err, output)
	}
}

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
	ciWorkflow := read(".github/workflows/ci.yml")
	installerWorkflow := read(".github/workflows/installers.yml")
	smokeWorkflow := read(".github/workflows/release-smoke.yml")
	if attributes := read(".gitattributes"); !strings.Contains(attributes, "* text=auto eol=lf") {
		t.Error("repository text files must retain LF line endings on every runner")
	}
	for _, required := range []string{"needs: test", "permissions: {}", "contents: write", "packages: write", "id-token: write", "--skip=homebrew", "--skip=scoop", "node-version: '24'", "npm@11.5.1", "npm_tag=latest", "npm_tag=next", "publish_if_missing()", `npm view "${package_name}@${package_version}" version`, `npm publish "./${package_dir}" --access public --tag "$npm_tag"`, "docker/setup-buildx-action@", "docker logout ghcr.io", "workflow_dispatch:", "Select GoReleaser configuration", `config_file="$RUNNER_TEMP/goreleaser-recovery.yaml"`, "sed -i '/^release:$/a\\  skip_upload: true' \"$config_file\"", "--config ${{ steps.release_config.outputs.path }}", "GORELEASER_CURRENT_TAG", "RELEASE_TAG: ${{ inputs.tag || github.ref_name }}", "tools/release-channels.py", "--pattern checksums.txt", "Formula/git-a2a.rb", "Casks/git-a2a.rb", "HOMEBREW_TAP_TOKEN", "SCOOP_BUCKET_TOKEN", "dist/mcpb/build.py", "mcp-publisher_linux_amd64.tar.gz", "github-oidc", "mcp-publisher publish mcp-dist/server.json"} {
		if !strings.Contains(workflow, required) {
			t.Errorf("release workflow missing %q", required)
		}
	}
	for _, forbidden := range []string{"NPM_TOKEN", "NODE_AUTH_TOKEN", "npm publishing skipped"} {
		if strings.Contains(workflow, forbidden) {
			t.Errorf("trusted npm publishing must not contain %q", forbidden)
		}
	}
	if regexp.MustCompile(`(?m)^\s*if:\s*\$\{\{\s*secrets\.`).MatchString(workflow) {
		t.Error("GitHub Actions does not allow the secrets context directly in an if expression")
	}
	for _, required := range []string{"live_channels:", "scoop-live:", "scoop install git-a2a/git-a2a", "channel=scoop", "Get-Content internal/version/VERSION"} {
		if !strings.Contains(installerWorkflow, required) {
			t.Errorf("installer workflow missing live Scoop check %q", required)
		}
	}
	if strings.Contains(installerWorkflow, `'^git-a2a 1\.0\.0`) {
		t.Error("live Scoop check must derive the expected version from internal/version/VERSION")
	}
	for _, required := range []string{"ubuntu-latest", "macos-latest", "windows-latest", "gh release download", `binary_version="${version%%-*}"`, "go run ./tools/mcp-smoke", "setup --harness codex --dry-run"} {
		if !strings.Contains(smokeWorkflow, required) {
			t.Errorf("release smoke workflow missing %q", required)
		}
	}
	if got := strings.Count(smokeWorkflow, "uses:"); got != 2 || strings.Count(smokeWorkflow, "# v") != got {
		t.Errorf("release smoke actions must be SHA-pinned with version comments: uses=%d comments=%d", got, strings.Count(smokeWorkflow, "# v"))
	}
	goReleaserEnv := regexp.MustCompile(`(?s)uses: goreleaser/goreleaser-action@[0-9a-f]{40}.*?env:\s+GORELEASER_CURRENT_TAG: \$\{\{ env\.RELEASE_TAG \}\}`)
	if !goReleaserEnv.MatchString(workflow) {
		t.Error("GoReleaser action must receive the explicitly selected immutable tag")
	}
	for name, body := range map[string]string{"release": workflow, "CI": ciWorkflow} {
		if !strings.Contains(body, "@openhint/cli@1.5.1 @openhint/hintbook-software-engineer@1.3.1") || !strings.Contains(body, "go test -count=1 ./...") {
			t.Errorf("%s workflow must install the pinned HINT compiler and bypass stale test results", name)
		}
	}
	allWorkflows := workflow + "\n" + ciWorkflow
	action := regexp.MustCompile(`(?m)^\s*- uses: [^@]+@[0-9a-f]{40}(?:\s+# v[^\s]+)$`)
	uses := regexp.MustCompile(`(?m)^\s*- uses:`).FindAllStringIndex(allWorkflows, -1)
	if matches := action.FindAllStringIndex(allWorkflows, -1); len(matches) != len(uses) {
		t.Errorf("every pinned action needs a version comment: uses=%d commented=%d", len(uses), len(matches))
	}
	if config := read(".goreleaser.yaml"); !strings.Contains(config, "prerelease: auto") || !strings.Contains(config, "go mod verify") {
		t.Error("GoReleaser prerelease or clean-tree gate is missing")
	}
	config := read(".goreleaser.yaml")
	for _, required := range []string{"title: Features", "title: Bug fixes", "title: Documentation", "release:", "header: |", "footer: |", "org.opencontainers.image.source", "io.modelcontextprotocol.server.name: io.github.neprel/git-a2a", "if not .Prerelease"} {
		if !strings.Contains(config, required) {
			t.Errorf("GoReleaser release presentation missing %q", required)
		}
	}
	if strings.Contains(config, "exclude: ['^docs:'") {
		t.Error("documentation changes must appear in categorized release notes")
	}
	if !strings.Contains(config, `'^[[:word:]-]+\(ci\)!?:'`) {
		t.Error("CI-scoped commits must not appear as user-visible release changes")
	}
	installer := read("install.sh")
	for _, required := range []string{"sha256sum", "url_effective", "mingw", "not writable", "version=\"v$version\""} {
		if !strings.Contains(strings.ToLower(installer), strings.ToLower(required)) {
			t.Errorf("installer missing %q", required)
		}
	}
	if docs := read("docs/releasing.md"); !strings.Contains(docs, "HOMEBREW_TAP_TOKEN") || !strings.Contains(docs, "SCOOP_BUCKET_TOKEN") || !strings.Contains(docs, "trusted publisher") || !strings.Contains(docs, "environment named `pypi`") {
		t.Error("release prerequisites are incomplete")
	}
	npmBuilder := read("dist/npm/build_packages.py")
	if strings.Count(npmBuilder, `"repository": REPOSITORY`) != 2 || !strings.Contains(npmBuilder, `"url": "git+https://github.com/neprel/git-a2a.git"`) {
		t.Error("every npm package must carry repository metadata matching the trusted publisher")
	}
	if !strings.Contains(npmBuilder, `"mcpName": "io.github.neprel/git-a2a"`) || !strings.Contains(read("dist/npm/package.json"), `"mcpName": "io.github.neprel/git-a2a"`) {
		t.Error("npm launcher metadata must prove MCP Registry ownership")
	}
	launcher := read("dist/pypi/git_a2a.py")
	if !strings.Contains(launcher, "os.execv") {
		t.Error("PyPI launcher must replace the Python process on POSIX")
	}
	wheelBuilder := read("dist/pypi/build_wheels.py")
	if strings.Contains(wheelBuilder, "LAUNCHER =") || !strings.Contains(wheelBuilder, `with_name("git_a2a.py").read_bytes()`) {
		t.Error("PyPI wheel builder must embed the canonical launcher source")
	}
	if !strings.Contains(wheelBuilder, "info.create_system = 3") || !strings.Contains(wheelBuilder, "0o100755") {
		t.Error("PyPI wheel entries must retain Unix executable modes")
	}
	command := exec.Command("sh", "-n", filepath.Join(root, "install.sh"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("install.sh syntax: %v: %s", err, output)
	}
}
