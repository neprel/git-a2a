package clojure_test

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPublicCLIClojureLifecycle exercises the Clojure adapter through the
// public five-command CLI. The pinned native runner enables this test; in that
// environment a missing clojure/clj executable is a hard failure, never a
// successful skip.
func TestPublicCLIClojureLifecycle(t *testing.T) {
	if os.Getenv("GITA2A_IT_CLOJURE_CLI") != "1" {
		t.Skip("set GITA2A_IT_CLOJURE_CLI=1 in the pinned Clojure environment")
	}
	for _, tool := range []string{"git", "clj", "clojure"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("required native tool %s: %v", tool, err)
		}
	}
	bin := clojureCLIBinary(t)
	base := t.TempDir()
	upstream := filepath.Join(base, "upstream")
	consumer := filepath.Join(base, "consumer")

	clojureWrite(t, upstream, "a2amodule.yml", `schema: 2
component:
  id: fixture-clojure
  exports:
    - adapter: clojure
      name: acme/fixture
agent:
  card: https://agents.example/fixture-clojure.json
`)
	clojureWrite(t, upstream, "deps.edn", "{:paths [\"src\"]}\n")
	clojureWrite(t, upstream, "src/acme/fixture.clj", "(ns acme.fixture)\n(def value \"v1\")\n")
	clojureGitInit(t, upstream)
	clojureGitCommit(t, upstream, "v1")

	clojureWrite(t, consumer, "deps.edn", `{:paths ["src"]
 :deps {unrelated/lib {:local/root "unrelated"}}}
`)
	clojureWrite(t, consumer, "unrelated/deps.edn", "{:paths [\"src\"]}\n")
	clojureWrite(t, consumer, "unrelated/src/unrelated/lib.clj", "(ns unrelated.lib)\n(def value \"kept\")\n")
	runClojureNative(t, consumer, bin, "init", "--id", "consumer")
	runClojureNative(t, consumer, bin, "add", clojureFileURL(upstream), "--name", "fixture", "--ref", "main")
	assertClojureUse(t, consumer, "v1/kept")
	first := clojureManagedSHA(t, consumer)
	assertClojureGitlib(t, "acme/fixture", first)

	// Pull at the same commit must repair project-local/native materialization.
	for _, path := range clojureGitlibPaths(t, "acme/fixture", first) {
		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
	}
	depsBeforeList := clojureRead(t, filepath.Join(consumer, "deps.edn"))
	lockBeforeList := clojureRead(t, filepath.Join(consumer, "a2amodule.lock"))
	listed := runClojureNative(t, consumer, bin, "list", "fixture")
	if !strings.Contains(listed, "problem:") || !strings.Contains(listed, "got missing") {
		t.Fatalf("list did not report missing Clojure materialization:\n%s", listed)
	}
	for _, path := range clojureGitlibPaths(t, "acme/fixture", first) {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("read-only list recreated Clojure gitlib checkout %s: %v", path, err)
		}
	}
	if got := clojureRead(t, filepath.Join(consumer, "deps.edn")); got != depsBeforeList {
		t.Fatal("read-only list changed deps.edn")
	}
	if got := clojureRead(t, filepath.Join(consumer, "a2amodule.lock")); got != lockBeforeList {
		t.Fatal("read-only list changed a2amodule.lock")
	}
	runClojureNative(t, consumer, bin, "pull", "fixture")
	assertClojureGitlib(t, "acme/fixture", first)
	assertClojureUse(t, consumer, "v1/kept")

	// A fresh clone has declarations and git-a2a lock, but no Git library
	// checkout. Public pull must make it usable without a manual clj command.
	clojureGitInit(t, consumer)
	clojureGitCommit(t, consumer, "installed")
	fresh := filepath.Join(base, "fresh")
	runClojureNative(t, base, "git", "clone", consumer, fresh)
	for _, home := range clojureHomes(t) {
		if err := os.RemoveAll(filepath.Join(home, ".gitlibs", "libs", "acme", "fixture")); err != nil {
			t.Fatal(err)
		}
	}
	runClojureNative(t, fresh, bin, "pull", "fixture")
	assertClojureUse(t, fresh, "v1/kept")

	// Advance the requested branch without changing package coordinates.
	clojureWrite(t, upstream, "src/acme/fixture.clj", "(ns acme.fixture)\n(def value \"v2\")\n")
	clojureGitCommit(t, upstream, "v2-same-coordinates")
	runClojureNative(t, consumer, bin, "pull", "fixture")
	second := clojureManagedSHA(t, consumer)
	if first == second {
		t.Fatalf("targeted pull did not advance commit %s", first)
	}
	assertClojureUse(t, consumer, "v2/kept")
	assertClojureGitlib(t, "acme/fixture", second)

	// Repeating pull at the same commit must not rewrite declarations or lock.
	depsBefore := clojureRead(t, filepath.Join(consumer, "deps.edn"))
	lockBefore := clojureRead(t, filepath.Join(consumer, "a2amodule.lock"))
	runClojureNative(t, consumer, bin, "pull", "fixture")
	if got := clojureRead(t, filepath.Join(consumer, "deps.edn")); got != depsBefore {
		t.Fatalf("idempotent pull rewrote deps.edn\nbefore:\n%s\nafter:\n%s", depsBefore, got)
	}
	if got := clojureRead(t, filepath.Join(consumer, "a2amodule.lock")); got != lockBefore {
		t.Fatalf("idempotent pull rewrote a2amodule.lock\nbefore:\n%s\nafter:\n%s", lockBefore, got)
	}

	runClojureNative(t, consumer, bin, "remove", "fixture")
	if strings.Contains(clojureRead(t, filepath.Join(consumer, "deps.edn")), "acme/fixture") {
		t.Fatal("removed root dependency remains in deps.edn")
	}
	if strings.Contains(clojureRead(t, filepath.Join(consumer, "a2amodule.lock")), "fixture") {
		t.Fatal("removed dependency remains in a2amodule.lock")
	}
	if got := strings.TrimSpace(runClojureNative(t, consumer, "clojure", "-M", "-e", `(require 'unrelated.lib) (print unrelated.lib/value)`)); got != "kept" {
		t.Fatalf("unrelated Clojure dependency output=%q want=%q", got, "kept")
	}
}

func clojureCLIBinary(t *testing.T) string {
	t.Helper()
	if binary := os.Getenv("GITA2A_CLI_BIN"); binary != "" {
		return binary
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	binary := filepath.Join(t.TempDir(), "git-a2a")
	runClojureNative(t, repo, "go", "build", "-o", binary, "./cmd/git-a2a")
	return binary
}

func assertClojureUse(t *testing.T, root, want string) {
	t.Helper()
	got := strings.TrimSpace(runClojureNative(t, root, "clojure", "-M", "-e", `(require 'acme.fixture 'unrelated.lib) (print (str acme.fixture/value "/" unrelated.lib/value))`))
	if got != want {
		t.Fatalf("Clojure consumer output=%q want=%q", got, want)
	}
}

func clojureManagedSHA(t *testing.T, root string) string {
	t.Helper()
	body := clojureRead(t, filepath.Join(root, "deps.edn"))
	marker := `:git/sha "`
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatalf("deps.edn has no managed :git/sha:\n%s", body)
	}
	start += len(marker)
	end := strings.Index(body[start:], `"`)
	if end < 0 {
		t.Fatalf("deps.edn has malformed managed :git/sha:\n%s", body)
	}
	sha := body[start : start+end]
	if len(sha) != 40 {
		t.Fatalf("managed :git/sha=%q, want full 40-character commit", sha)
	}
	return sha
}

func assertClojureGitlib(t *testing.T, name, commit string) {
	t.Helper()
	for _, path := range clojureGitlibPaths(t, name, commit) {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return
		}
	}
	t.Fatalf("Clojure gitlib checkout is not materialized in %v", clojureGitlibPaths(t, name, commit))
}

func clojureGitlibPaths(t *testing.T, name, commit string) []string {
	t.Helper()
	var paths []string
	for _, home := range clojureHomes(t) {
		paths = append(paths, filepath.Join(home, ".gitlibs", "libs", filepath.FromSlash(name), commit))
	}
	return paths
}

func clojureHomes(t *testing.T) []string {
	t.Helper()
	var homes []string
	if current, err := user.Current(); err == nil && current.HomeDir != "" {
		homes = append(homes, current.HomeDir)
	}
	home, err := os.UserHomeDir()
	if err != nil && len(homes) == 0 {
		t.Fatal(err)
	}
	if home != "" && (len(homes) == 0 || homes[0] != home) {
		homes = append(homes, home)
	}
	return homes
}

func clojureGitInit(t *testing.T, root string) {
	t.Helper()
	runClojureNative(t, root, "git", "init", "-b", "main")
	runClojureNative(t, root, "git", "config", "user.email", "native@example.test")
	runClojureNative(t, root, "git", "config", "user.name", "Native")
}

func clojureGitCommit(t *testing.T, root, message string) {
	t.Helper()
	runClojureNative(t, root, "git", "add", "-A")
	runClojureNative(t, root, "git", "commit", "-m", message)
}

func clojureWrite(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func clojureRead(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func clojureFileURL(path string) string {
	path = filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		return "file:///" + strings.TrimPrefix(path, "/")
	}
	return "file://" + path
}

func runClojureNative(t *testing.T, dir, name string, args ...string) string {
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
