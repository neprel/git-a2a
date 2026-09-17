package submodule_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPublicCLISubmoduleLifecycle is gated because it builds the public CLI and
// creates real Git repositories and submodules. The pinned native runner sets
// GITA2A_IT_SUBMODULE_CLI=1; once enabled, missing tools are test failures.
func TestPublicCLISubmoduleLifecycle(t *testing.T) {
	if os.Getenv("GITA2A_IT_SUBMODULE_CLI") != "1" {
		t.Skip("set GITA2A_IT_SUBMODULE_CLI=1 in the pinned native environment")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("required native tool git: %v", err)
	}

	bin := buildCLI(t)
	upstream := newRepository(t, map[string]string{
		"a2amodule.yml": "schema: 2\ncomponent:\n  id: native-submodule\n  exports:\n    - adapter: submodule\n      name: source\nagent:\n  card: https://agents.example/native-submodule.json\n",
		"content.txt":   "one\n",
	})
	first := gitOutput(t, upstream, "rev-parse", "HEAD")
	unrelated := newRepository(t, map[string]string{"unrelated.txt": "preserve me\n"})
	unrelatedFirst := gitOutput(t, unrelated, "rev-parse", "HEAD")

	consumer := t.TempDir()
	gitInit(t, consumer)
	writeFile(t, consumer, "README.md", "consumer\n")
	run(t, consumer, "git", "-c", "protocol.file.allow=always", "submodule", "add", "--", fileURL(unrelated), "deps/unrelated")
	gitCommit(t, consumer, "consumer baseline")

	run(t, consumer, bin, "init", "--id", "consumer")
	run(t, consumer, bin, "add", fileURL(upstream), "--name", "fixture", "--ref", "main")
	assertSubmodule(t, consumer, "deps/fixture", first, "content.txt", "one\n", true)
	assertSubmodule(t, consumer, "deps/unrelated", unrelatedFirst, "unrelated.txt", "preserve me\n", false)
	assertManifestBinding(t, consumer, "fixture", "submodule", "git")
	gitCommit(t, consumer, "installed fixture")

	// A normal clone has the gitlink and .gitmodules entry but no initialized
	// checkout. Public pull must initialize it and materialize the locked commit.
	fresh := filepath.Join(t.TempDir(), "fresh")
	run(t, filepath.Dir(fresh), "git", "clone", "--", consumer, fresh)
	if _, err := os.Stat(filepath.Join(fresh, "deps", "fixture", ".git")); !os.IsNotExist(err) {
		t.Fatalf("fresh clone unexpectedly initialized fixture submodule: %v", err)
	}
	run(t, fresh, bin, "pull", "fixture")
	assertSubmodule(t, fresh, "deps/fixture", first, "content.txt", "one\n", true)

	// Losing only the checkout must be repairable without changing the locked
	// revision or requiring a separate git submodule command.
	if err := os.RemoveAll(filepath.Join(consumer, "deps", "fixture")); err != nil {
		t.Fatal(err)
	}
	run(t, consumer, bin, "pull", "fixture")
	assertSubmodule(t, consumer, "deps/fixture", first, "content.txt", "one\n", true)

	// Advance both remotes. A targeted pull of fixture must update exactly that
	// dependency while leaving the unrelated submodule at its existing commit.
	writeFile(t, upstream, "content.txt", "two\n")
	gitCommit(t, upstream, "same-version source update")
	second := gitOutput(t, upstream, "rev-parse", "HEAD")
	writeFile(t, unrelated, "unrelated.txt", "remote changed\n")
	gitCommit(t, unrelated, "unrelated remote update")
	run(t, consumer, bin, "pull", "fixture")
	assertSubmodule(t, consumer, "deps/fixture", second, "content.txt", "two\n", true)
	assertSubmodule(t, consumer, "deps/unrelated", unrelatedFirst, "unrelated.txt", "preserve me\n", false)

	run(t, consumer, bin, "remove", "fixture")
	assertRemovedCleanly(t, consumer, "deps/fixture")
	assertSubmodule(t, consumer, "deps/unrelated", unrelatedFirst, "unrelated.txt", "preserve me\n", false)
	modules := mustRead(t, filepath.Join(consumer, ".gitmodules"))
	if strings.Contains(modules, "deps/fixture") || !strings.Contains(modules, "deps/unrelated") {
		t.Fatalf("unexpected .gitmodules after targeted remove:\n%s", modules)
	}
	manifest := mustRead(t, filepath.Join(consumer, "a2amodule.yml"))
	if strings.Contains(manifest, "name: fixture") {
		t.Fatalf("dependency declaration survived remove:\n%s", manifest)
	}
}

func buildCLI(t *testing.T) string {
	t.Helper()
	if binary := os.Getenv("GITA2A_CLI_BIN"); binary != "" {
		info, err := os.Stat(binary)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			t.Fatalf("GITA2A_CLI_BIN %q is not an executable file: %v", binary, err)
		}
		return binary
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatalf("required native tool go (or set GITA2A_CLI_BIN): %v", err)
	}
	_, here, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
	bin := filepath.Join(t.TempDir(), "git-a2a")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	run(t, repo, "go", "build", "-o", bin, "./cmd/git-a2a")
	return bin
}

func newRepository(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	gitInit(t, root)
	for name, body := range files {
		writeFile(t, root, name, body)
	}
	gitCommit(t, root, "initial")
	return root
}

func gitInit(t *testing.T, root string) {
	t.Helper()
	run(t, root, "git", "init", "-b", "main")
	run(t, root, "git", "config", "user.name", "git-a2a native test")
	run(t, root, "git", "config", "user.email", "git-a2a@example.invalid")
}

func gitCommit(t *testing.T, root, message string) {
	t.Helper()
	run(t, root, "git", "add", "-A")
	run(t, root, "git", "commit", "-m", message)
}

func assertSubmodule(t *testing.T, root, relative, wantCommit, contentPath, wantContent string, wantDetached bool) {
	t.Helper()
	fields := strings.Fields(gitOutput(t, root, "ls-files", "-s", "--", relative))
	if len(fields) < 2 || fields[0] != "160000" || fields[1] != wantCommit {
		t.Fatalf("gitlink %s = %v, want 160000 %s", relative, fields, wantCommit)
	}
	dest := filepath.Join(root, filepath.FromSlash(relative))
	if got := gitOutput(t, dest, "rev-parse", "HEAD"); got != wantCommit {
		t.Fatalf("checkout %s HEAD = %s, want %s", relative, got, wantCommit)
	}
	if got := gitOutput(t, dest, "rev-parse", "--abbrev-ref", "HEAD"); wantDetached && got != "HEAD" {
		t.Fatalf("checkout %s is attached to %s", relative, got)
	}
	if got := mustRead(t, filepath.Join(dest, filepath.FromSlash(contentPath))); got != wantContent {
		t.Fatalf("%s/%s = %q, want %q", relative, contentPath, got, wantContent)
	}
}

func assertManifestBinding(t *testing.T, root, name, adapter, variant string) {
	t.Helper()
	body := mustRead(t, filepath.Join(root, "a2amodule.yml"))
	for _, want := range []string{"name: " + name, "adapter: " + adapter, "variant: " + variant} {
		if !strings.Contains(body, want) {
			t.Fatalf("manifest lacks %q:\n%s", want, body)
		}
	}
}

func assertRemovedCleanly(t *testing.T, root, relative string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relative))); !os.IsNotExist(err) {
		t.Fatalf("removed checkout %s remains: %v", relative, err)
	}
	if got := gitOutput(t, root, "ls-files", "-s", "--", relative); got != "" {
		t.Fatalf("removed gitlink %s remains: %q", relative, got)
	}
	store := filepath.Join(root, ".git", "modules", filepath.FromSlash(relative))
	if _, err := os.Lstat(store); !os.IsNotExist(err) {
		t.Fatalf("removed module store %s remains: %v", store, err)
	}
	config := mustRead(t, filepath.Join(root, ".git", "config"))
	if strings.Contains(config, relative) {
		t.Fatalf("removed submodule local config for %s remains:\n%s", relative, config)
	}
}

func writeFile(t *testing.T, root, relative, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func gitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	return strings.TrimSpace(run(t, root, "git", args...))
}

func fileURL(path string) string {
	slashed := filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		return "file:///" + strings.TrimPrefix(slashed, "/")
	}
	return "file://" + slashed
}

func run(t *testing.T, dir, command string, args ...string) string {
	t.Helper()
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", command, strings.Join(args, " "), err, out)
	}
	return string(out)
}
