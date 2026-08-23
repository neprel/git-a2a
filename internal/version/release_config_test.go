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
	ciWorkflow := read(".github/workflows/ci.yml")
	if attributes := read(".gitattributes"); !strings.Contains(attributes, "* text=auto eol=lf") {
		t.Error("repository text files must retain LF line endings on every runner")
	}
	for _, required := range []string{"needs: test", "permissions: {}", "contents: write", "packages: write", "id-token: write", "--skip=homebrew", "--skip=scoop", "node-version: '24'", "npm@11.5.1", "npm_tag=latest", "npm_tag=next", `npm publish "./$package" --access public --tag "$npm_tag"`, `npm publish ./npm-packages/git-a2a --access public --tag "$npm_tag"`, "docker/setup-buildx-action@", "docker logout ghcr.io", "workflow_dispatch:", "Select GoReleaser configuration", `config_file="$RUNNER_TEMP/goreleaser-recovery.yaml"`, "sed -i '/^release:$/a\\  disable: true' \"$config_file\"", "--config ${{ steps.release_config.outputs.path }}", "GORELEASER_CURRENT_TAG", "RELEASE_TAG: ${{ inputs.tag || github.ref_name }}"} {
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
	for _, required := range []string{"title: Features", "title: Bug fixes", "title: Documentation", "release:", "header: |", "footer: |", "org.opencontainers.image.source", "if not .Prerelease"} {
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
