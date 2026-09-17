package gem

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPublicCLIBundlerLifecycle is gated because it executes the real public
// CLI, Git, Ruby, and Bundler. A native job that enables it treats a missing
// advertised tool as a failure rather than silently reducing coverage.
func TestPublicCLIBundlerLifecycle(t *testing.T) {
	if os.Getenv("GITA2A_IT_GEM_CLI") != "1" {
		t.Skip("set GITA2A_IT_GEM_CLI=1 to exercise the public CLI with Bundler")
	}
	tools := []string{"git", "ruby", "bundle"}
	if os.Getenv("GITA2A_CLI_BIN") == "" {
		tools = append(tools, "go")
	}
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("required native tool %s: %v", tool, err)
		}
	}

	bin := buildBundlerCLI(t)
	upstream := newBundlerUpstream(t, "one")
	source := bundlerFileURL(upstream)

	consumer := t.TempDir()
	writeBundlerFile(t, filepath.Join(consumer, "Gemfile"), `source "https://rubygems.org"

gem "unrelated-gem", path: "./unrelated-gem"
`)
	writeBundlerFile(t, filepath.Join(consumer, ".bundle", "config"), "---\nBUNDLE_PATH: \".bundle/gems\"\nBUNDLE_DISABLE_SHARED_GEMS: \"true\"\n")
	writeBundlerGem(t, filepath.Join(consumer, "unrelated-gem"), "unrelated-gem", "unrelated_gem", "seven")

	runBundlerNative(t, consumer, bin, "init", "--id", "bundler-consumer")
	runBundlerNative(t, consumer, bin, "add", source, "--name", "native-lib", "--ref", "main")
	assertBundlerValue(t, consumer, "native_lib", "NativeLib::VALUE", "one")
	assertBundlerValue(t, consumer, "unrelated_gem", "UnrelatedGem::VALUE", "seven")
	assertBundlerRevision(t, consumer, bundlerHead(t, upstream))

	// A clone contains declarations and locks, but not the ignored local bundle.
	gitBundlerInit(t, consumer)
	runBundlerNative(t, consumer, "git", "add", ".gitignore", "a2amodule.yml", "a2amodule.lock", "Gemfile", "Gemfile.lock", ".bundle/config", "unrelated-gem")
	runBundlerNative(t, consumer, "git", "commit", "-m", "installed")
	fresh := filepath.Join(t.TempDir(), "fresh")
	runBundlerNative(t, "", "git", "clone", consumer, fresh)
	runBundlerNative(t, fresh, bin, "pull", "native-lib")
	assertBundlerValue(t, fresh, "native_lib", "NativeLib::VALUE", "one")
	assertBundlerValue(t, fresh, "unrelated_gem", "UnrelatedGem::VALUE", "seven")

	// Losing all installed gems must be repaired even when the selected branch
	// still resolves to the exact commit already present in both locks.
	if err := os.RemoveAll(filepath.Join(consumer, ".bundle", "gems")); err != nil {
		t.Fatal(err)
	}
	runBundlerNative(t, consumer, bin, "pull", "native-lib")
	assertBundlerValue(t, consumer, "native_lib", "NativeLib::VALUE", "one")
	assertBundlerValue(t, consumer, "unrelated_gem", "UnrelatedGem::VALUE", "seven")

	// The gem version stays 1.0.0. Pull must follow the branch to the new Git
	// commit rather than accepting an installation solely because versions match.
	writeBundlerImplementation(t, upstream, "two")
	runBundlerNative(t, upstream, "git", "add", "lib/native_lib.rb")
	runBundlerNative(t, upstream, "git", "commit", "-m", "same-version advance")
	second := bundlerHead(t, upstream)
	runBundlerNative(t, consumer, bin, "pull", "native-lib")
	assertBundlerValue(t, consumer, "native_lib", "NativeLib::VALUE", "two")
	assertBundlerValue(t, consumer, "unrelated_gem", "UnrelatedGem::VALUE", "seven")
	assertBundlerRevision(t, consumer, second)

	runBundlerNative(t, consumer, bin, "remove", "native-lib")
	if body := string(readBundlerFile(t, filepath.Join(consumer, "Gemfile"))); strings.Contains(body, `gem "native-lib"`) {
		t.Fatalf("removed root declaration remains:\n%s", body)
	}
	if body := string(readBundlerFile(t, filepath.Join(consumer, "Gemfile.lock"))); strings.Contains(body, source) || strings.Contains(body, "native-lib (") {
		t.Fatalf("removed git dependency remains in native lock:\n%s", body)
	}
	assertBundlerRequireFails(t, consumer, "native_lib")
	assertBundlerValue(t, consumer, "unrelated_gem", "UnrelatedGem::VALUE", "seven")
}

func buildBundlerCLI(t *testing.T) string {
	t.Helper()
	if binary := os.Getenv("GITA2A_CLI_BIN"); binary != "" {
		return binary
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	binary := filepath.Join(t.TempDir(), "git-a2a")
	runBundlerNative(t, repo, "go", "build", "-o", binary, "./cmd/git-a2a")
	return binary
}

func newBundlerUpstream(t *testing.T, value string) string {
	t.Helper()
	root := t.TempDir()
	gitBundlerInit(t, root)
	writeBundlerFile(t, filepath.Join(root, "a2amodule.yml"), "schema: 2\ncomponent:\n  id: native-lib\n  exports:\n    - adapter: gem\n      name: native-lib\nagent:\n  card: https://agents.example/native-lib.json\n")
	writeBundlerFile(t, filepath.Join(root, "native-lib.gemspec"), `Gem::Specification.new do |spec|
  spec.name = "native-lib"
  spec.version = "1.0.0"
  spec.summary = "git-a2a native Bundler fixture"
  spec.authors = ["git-a2a"]
  spec.files = ["lib/native_lib.rb"]
  spec.require_paths = ["lib"]
end
`)
	writeBundlerImplementation(t, root, value)
	runBundlerNative(t, root, "git", "add", ".")
	runBundlerNative(t, root, "git", "commit", "-m", "initial")
	return root
}

func writeBundlerGem(t *testing.T, root, gemName, requireName, value string) {
	t.Helper()
	writeBundlerFile(t, filepath.Join(root, gemName+".gemspec"), `Gem::Specification.new do |spec|
  spec.name = "`+gemName+`"
  spec.version = "7.0.0"
  spec.summary = "unrelated fixture"
  spec.authors = ["git-a2a"]
  spec.files = ["lib/`+requireName+`.rb"]
  spec.require_paths = ["lib"]
end
`)
	writeBundlerFile(t, filepath.Join(root, "lib", requireName+".rb"), "module UnrelatedGem\n  VALUE = \""+value+"\"\nend\n")
}

func writeBundlerImplementation(t *testing.T, root, value string) {
	t.Helper()
	writeBundlerFile(t, filepath.Join(root, "lib", "native_lib.rb"), "module NativeLib\n  VALUE = \""+value+"\"\nend\n")
}

func assertBundlerValue(t *testing.T, root, requireName, expression, want string) {
	t.Helper()
	got := strings.TrimSpace(runBundlerNative(t, root, "bundle", "exec", "ruby", "-e", `require "`+requireName+`"; print `+expression))
	if got != want {
		t.Fatalf("%s=%q, want %q", expression, got, want)
	}
}

func assertBundlerRequireFails(t *testing.T, root, requireName string) {
	t.Helper()
	cmd := exec.Command("bundle", "exec", "ruby", "-e", `require "`+requireName+`"`)
	cmd.Dir = root
	cmd.Env = bundlerNativeEnv()
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("removed gem %s remains requireable:\n%s", requireName, output)
	}
}

func assertBundlerRevision(t *testing.T, root, want string) {
	t.Helper()
	body := string(readBundlerFile(t, filepath.Join(root, "Gemfile.lock")))
	if !strings.Contains(body, "  revision: "+want+"\n") {
		t.Fatalf("Gemfile.lock does not contain revision %s:\n%s", want, body)
	}
}

func gitBundlerInit(t *testing.T, root string) {
	t.Helper()
	runBundlerNative(t, root, "git", "init", "-b", "main")
	runBundlerNative(t, root, "git", "config", "user.name", "git-a2a native")
	runBundlerNative(t, root, "git", "config", "user.email", "native@example.invalid")
}

func bundlerHead(t *testing.T, root string) string {
	t.Helper()
	return strings.TrimSpace(runBundlerNative(t, root, "git", "rev-parse", "HEAD"))
}

func bundlerFileURL(path string) string {
	path = filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		return "file:///" + strings.TrimPrefix(path, "/")
	}
	return "file://" + path
}

func writeBundlerFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readBundlerFile(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func runBundlerNative(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = bundlerNativeEnv()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func bundlerNativeEnv() []string {
	return append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=file")
}
