package npm

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPublicCLINPMVariantLifecycle exercises the public five-command surface.
// Native jobs opt in explicitly; once enabled, a missing advertised manager is
// a hard failure from the CLI rather than a skip.
func TestPublicCLINPMVariantLifecycle(t *testing.T) {
	if os.Getenv("GITA2A_IT_NPM_CLI") != "1" {
		t.Skip("set GITA2A_IT_NPM_CLI=1 to exercise public CLI npm variants")
	}
	bin := buildNPMCLI(t)
	for _, tc := range []struct {
		name, manager string
	}{
		{name: "npm", manager: "npm@" + npmManagerVersion("GITA2A_NPM_VERSION", "11.18.0")},
		{name: "yarn-berry", manager: "yarn@" + npmManagerVersion("GITA2A_YARN_VERSION", "4.17.0")},
		{name: "pnpm", manager: "pnpm@" + npmManagerVersion("GITA2A_PNPM_VERSION", "9.7.0")},
		{name: "bun", manager: "bun@" + npmManagerVersion("GITA2A_BUN_VERSION", "1.4.2")},
	} {
		t.Run(tc.name, func(t *testing.T) { exerciseNPMCLI(t, bin, tc.name, tc.manager) })
	}
}

func npmManagerVersion(environment, fallback string) string {
	if value := os.Getenv(environment); value != "" {
		return value
	}
	return fallback
}

func exerciseNPMCLI(t *testing.T, bin, variant, packageManager string) {
	t.Helper()
	fixture := newNPMGitFixture(t)
	upstream := fixture.root
	writeNativeFile(t, filepath.Join(upstream, "a2amodule.yml"), "schema: 2\ncomponent:\n  id: native-lib\n  exports:\n    - adapter: npm\n      name: '@native/lib'\nagent:\n  card: https://agents.example/native-lib.json\n")
	runNative(t, upstream, "git", "add", "a2amodule.yml")
	runNative(t, upstream, "git", "commit", "-m", "manifest")
	source := fixture.url

	consumer := t.TempDir()
	unrelated := filepath.Join(consumer, "unrelated")
	writeNativeFile(t, filepath.Join(unrelated, "package.json"), `{"name":"unrelated","version":"7.0.0","main":"index.js"}`+"\n")
	writeNativeFile(t, filepath.Join(unrelated, "index.js"), "module.exports = { value: 7 };\n")
	document := map[string]any{
		"name":           "native-consumer",
		"private":        true,
		"packageManager": packageManager,
		"dependencies":   map[string]string{"unrelated": "file:./unrelated"},
	}
	body, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeNativeFile(t, filepath.Join(consumer, "package.json"), string(body)+"\n")
	if variant == "yarn-berry" {
		writeNativeFile(t, filepath.Join(consumer, ".yarnrc.yml"), "nodeLinker: node-modules\napprovedGitRepositories:\n  - '"+source+"'\n")
	}
	runNative(t, consumer, bin, "init", "--id", "native-consumer")
	runNative(t, consumer, bin, "add", source, "--name", "native-lib", "--ref", "main")
	assertNodeValue(t, consumer, "@native/lib", "one")
	assertNodeValue(t, consumer, "unrelated", "7")

	if err := os.RemoveAll(filepath.Join(consumer, "node_modules")); err != nil {
		t.Fatal(err)
	}
	runNative(t, consumer, bin, "pull", "native-lib")
	assertNodeValue(t, consumer, "@native/lib", "one")

	runNative(t, consumer, "git", "init", "-b", "main")
	runNative(t, consumer, "git", "config", "user.name", "git-a2a native")
	runNative(t, consumer, "git", "config", "user.email", "native@example.invalid")
	runNative(t, consumer, "git", "add", ".gitignore", "a2amodule.yml", "a2amodule.lock", "package.json", "unrelated")
	for _, file := range []string{".yarnrc.yml", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "bun.lock", "bun.lockb"} {
		if _, err := os.Stat(filepath.Join(consumer, file)); err == nil {
			runNative(t, consumer, "git", "add", file)
		}
	}
	runNative(t, consumer, "git", "commit", "-m", "installed")
	fresh := filepath.Join(t.TempDir(), "fresh")
	runNative(t, "", "git", "clone", consumer, fresh)
	runNative(t, fresh, bin, "pull", "native-lib")
	assertNodeValue(t, fresh, "@native/lib", "one")

	writeNativeFile(t, filepath.Join(upstream, "index.js"), "module.exports = { value: 'two' };\n")
	runNative(t, upstream, "git", "commit", "-am", "two")
	runNative(t, consumer, bin, "pull", "native-lib")
	assertNodeValue(t, consumer, "@native/lib", "two")
	assertNodeValue(t, consumer, "unrelated", "7")

	runNative(t, consumer, bin, "remove", "native-lib")
	assertNodeMissing(t, consumer, "@native/lib")
	assertNodeValue(t, consumer, "unrelated", "7")
	if strings.Contains(string(readNativeFile(t, filepath.Join(consumer, "package.json"))), `"@native/lib"`) {
		t.Fatal("removed root declaration remains")
	}
}

func buildNPMCLI(t *testing.T) string {
	t.Helper()
	if binary := os.Getenv("GITA2A_CLI_BIN"); binary != "" {
		return binary
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	binary := filepath.Join(t.TempDir(), "git-a2a")
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/git-a2a")
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build public CLI: %v\n%s", err, output)
	}
	return binary
}
