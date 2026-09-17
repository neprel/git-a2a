package hex

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPublicCLIHexLifecycle exercises Hex through the public six-command CLI.
// The pinned Docker runner sets GITA2A_NATIVE_HEX=1; a missing native tool is
// a failure once the test is enabled, never a successful skip.
func TestPublicCLIHexLifecycle(t *testing.T) {
	if os.Getenv("GITA2A_NATIVE_HEX") != "1" {
		t.Skip("set GITA2A_NATIVE_HEX=1 in the pinned Hex environment")
	}
	for _, tool := range []string{"git", "mix"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("required native tool %s: %v", tool, err)
		}
	}
	bin := nativeHexCLI(t)

	base := t.TempDir()
	upstream := filepath.Join(base, "upstream")
	writeHexNative(t, filepath.Join(upstream, "a2amodule.yml"), "schema: 2\ncomponent:\n  id: fixture-hex\n  exports:\n    - adapter: hex\n      name: fixture_hex\nagent:\n  card: https://agents.example/fixture-hex.json\n")
	writeHexNative(t, filepath.Join(upstream, "mix.exs"), `defmodule FixtureHex.MixProject do
  use Mix.Project
  def project, do: [app: :fixture_hex, version: "1.0.0", elixir: "~> 1.15"]
  def application, do: [extra_applications: [:logger]]
end
`)
	writeHexNative(t, filepath.Join(upstream, "lib", "fixture_hex.ex"), "defmodule FixtureHex do\n  def value, do: \"v1\"\nend\n")
	initHexGit(t, upstream)
	firstCommit := strings.TrimSpace(runHexNative(t, upstream, "git", "rev-parse", "HEAD"))

	consumer := filepath.Join(base, "consumer")
	writeHexNative(t, filepath.Join(consumer, "mix.exs"), `defmodule Consumer.MixProject do
  use Mix.Project
  def project, do: [app: :consumer, version: "1.0.0", elixir: "~> 1.15", deps: deps()]
  def application, do: [extra_applications: [:logger]]
  defp deps do
    [
      {:unrelated, path: "unrelated"}
    ]
  end
end
`)
	writeHexNative(t, filepath.Join(consumer, "unrelated", "mix.exs"), `defmodule Unrelated.MixProject do
  use Mix.Project
  def project, do: [app: :unrelated, version: "1.0.0", elixir: "~> 1.15"]
end
`)
	writeHexNative(t, filepath.Join(consumer, "unrelated", "src", "unrelated.erl"), "-module(unrelated).\n-export([value/0]).\nvalue() -> <<\"kept\">>.\n")

	runHexNative(t, consumer, bin, "init", "--id", "consumer")
	runHexNative(t, consumer, bin, "add", hexFileURL(upstream), "--name", "fixture", "--ref", "main")
	assertHexUse(t, consumer, "v1/kept")
	assertHexRevision(t, consumer, firstCommit)

	// A fresh clone contains declarations and locks but no manager-owned
	// materialization. Public pull must recreate a usable dependency.
	initHexGit(t, consumer)
	fresh := filepath.Join(base, "fresh")
	runHexNative(t, base, "git", "clone", consumer, fresh)
	if err := os.RemoveAll(filepath.Join(fresh, "deps")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(fresh, "_build")); err != nil {
		t.Fatal(err)
	}
	runHexNative(t, fresh, bin, "pull", "fixture")
	assertHexUse(t, fresh, "v1/kept")
	assertHexRevision(t, fresh, firstCommit)

	// Repair the same locked revision after both checkout and compiled output
	// disappear. The native lock remains stable.
	lockBeforeRepair := readHexNative(t, filepath.Join(consumer, "mix.lock"))
	if err := os.RemoveAll(filepath.Join(consumer, "deps", "fixture_hex")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(consumer, "_build")); err != nil {
		t.Fatal(err)
	}
	runHexNative(t, consumer, bin, "pull", "fixture")
	assertHexUse(t, consumer, "v1/kept")
	if got := readHexNative(t, filepath.Join(consumer, "mix.lock")); got != lockBeforeRepair {
		t.Fatalf("same-commit repair changed mix.lock:\n%s", got)
	}

	// Advance the branch without changing the package version and target only
	// this dependency. Both executable code and Mix's lock must move together.
	writeHexNative(t, filepath.Join(upstream, "lib", "fixture_hex.ex"), "defmodule FixtureHex do\n  def value, do: \"v2\"\nend\n")
	runHexNative(t, upstream, "git", "commit", "-am", "same-version v2")
	secondCommit := strings.TrimSpace(runHexNative(t, upstream, "git", "rev-parse", "HEAD"))
	if secondCommit == firstCommit {
		t.Fatal("upstream revision did not advance")
	}
	runHexNative(t, consumer, bin, "pull", "fixture")
	assertHexUse(t, consumer, "v2/kept")
	assertHexRevision(t, consumer, secondCommit)

	// Once converged, another targeted pull must not rewrite declarations or
	// either lock. This catches needless native-manager churn.
	stable := map[string]string{}
	for _, name := range []string{"mix.exs", "mix.lock", "a2amodule.yml", "a2amodule.lock"} {
		stable[name] = readHexNative(t, filepath.Join(consumer, name))
	}
	runHexNative(t, consumer, bin, "pull", "fixture")
	for name, want := range stable {
		if got := readHexNative(t, filepath.Join(consumer, name)); got != want {
			t.Fatalf("idempotent pull changed %s", name)
		}
	}

	runHexNative(t, consumer, bin, "remove", "fixture")
	if body := readHexNative(t, filepath.Join(consumer, "mix.exs")); strings.Contains(body, "fixture_hex") {
		t.Fatalf("removed root declaration remains:\n%s", body)
	}
	if body := readHexNative(t, filepath.Join(consumer, "mix.lock")); strings.Contains(body, "fixture_hex") {
		t.Fatalf("removed root lock entry remains:\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(consumer, "deps", "fixture_hex")); !os.IsNotExist(err) {
		t.Fatalf("removed checkout remains: %v", err)
	}
	if got := strings.TrimSpace(runHexNative(t, consumer, "mix", "run", "-e", "IO.write(:unrelated.value())")); !strings.HasSuffix(got, "kept") {
		t.Fatalf("unrelated dependency output=%q", got)
	}
}

func nativeHexCLI(t *testing.T) string {
	t.Helper()
	if binary := os.Getenv("GITA2A_CLI_BIN"); binary != "" {
		info, err := os.Stat(binary)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			t.Fatalf("GITA2A_CLI_BIN must name an executable: %s: %v", binary, err)
		}
		return binary
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	binary := filepath.Join(t.TempDir(), "git-a2a")
	runHexNative(t, repo, "go", "build", "-o", binary, "./cmd/git-a2a")
	return binary
}

func initHexGit(t *testing.T, root string) {
	t.Helper()
	runHexNative(t, root, "git", "init", "-b", "main")
	runHexNative(t, root, "git", "config", "user.email", "native@example.test")
	runHexNative(t, root, "git", "config", "user.name", "Native")
	runHexNative(t, root, "git", "add", "-A")
	runHexNative(t, root, "git", "commit", "-m", "initial")
}

func assertHexUse(t *testing.T, root, want string) {
	t.Helper()
	runHexNative(t, root, "mix", "compile")
	got := strings.TrimSpace(runHexNative(t, root, "mix", "run", "--no-compile", "-e", "IO.write(FixtureHex.value() <> \"/\" <> :unrelated.value())"))
	if !strings.HasSuffix(got, want) {
		t.Fatalf("Hex consumer output=%q want suffix %q", got, want)
	}
}

func assertHexRevision(t *testing.T, root, commit string) {
	t.Helper()
	for _, name := range []string{"mix.lock", "a2amodule.lock"} {
		body := readHexNative(t, filepath.Join(root, name))
		if !strings.Contains(body, commit) {
			t.Fatalf("%s does not record applied commit %s:\n%s", name, commit, body)
		}
	}
}

func hexFileURL(path string) string {
	path = filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		return "file:///" + strings.TrimPrefix(path, "/")
	}
	return "file://" + path
}

func writeHexNative(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readHexNative(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func runHexNative(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_ALLOW_PROTOCOL=file", "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}
