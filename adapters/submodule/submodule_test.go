package submodule

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/adapter"
)

func TestLifecycleExactDetachedCommitsAndSafeRemoval(t *testing.T) {
	fixture := newFixture(t)
	root := initRepository(t)
	a := Adapter{}
	dep := adapter.Dependency{Name: "library", Git: fixture.url, Ref: "main"}
	exp := adapter.Export{Adapter: "submodule", Name: "library", Path: "deps/library"}
	locked := adapter.Locked{Git: fixture.url, Ref: "main", Commit: fixture.first}

	change, err := a.Pull(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed {
		t.Fatalf("Wire() = %#v, %v", change, err)
	}
	assertExactState(t, root, "deps/library", fixture.url, fixture.first)
	if findings, driftErr := a.Inspect(context.Background(), root, dep, exp, locked); driftErr != nil || len(findings) != 0 {
		t.Fatalf("Drift() = %#v, %v", findings, driftErr)
	}
	change, err = a.Pull(context.Background(), root, dep, exp, locked)
	if err != nil || change.Changed {
		t.Fatalf("second Wire() = %#v, %v", change, err)
	}

	second := fixture.commit(t, "two\n")
	next := locked
	next.Commit = second
	if err = os.WriteFile(filepath.Join(root, "deps", "library", "user.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Pull(context.Background(), root, dep, exp, next); err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("dirty Pull() error = %v", err)
	}
	if got := git(t, filepath.Join(root, "deps", "library"), "rev-parse", "HEAD"); got != fixture.first {
		t.Fatalf("dirty refresh moved HEAD to %s", got)
	}
	if err = os.Remove(filepath.Join(root, "deps", "library", "user.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Pull(context.Background(), root, dep, exp, next); err != nil {
		t.Fatal(err)
	}
	assertExactState(t, root, "deps/library", fixture.url, second)

	if err = os.WriteFile(filepath.Join(root, "deps", "library", "user.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Remove(context.Background(), root, dep, exp, next); err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("dirty Unwire() error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "deps", "library")); statErr != nil {
		t.Fatalf("dirty removal erased checkout: %v", statErr)
	}
	if err = os.Remove(filepath.Join(root, "deps", "library", "user.txt")); err != nil {
		t.Fatal(err)
	}
	git(t, root, "commit", "-am", "pin dependency")
	change, err = a.Remove(context.Background(), root, dep, exp, next)
	if err != nil || !change.Changed {
		t.Fatalf("Unwire() = %#v, %v", change, err)
	}
	if _, statErr := os.Lstat(filepath.Join(root, "deps", "library")); !os.IsNotExist(statErr) {
		t.Fatalf("submodule worktree remains: %v", statErr)
	}
	if _, statErr := os.Lstat(filepath.Join(root, ".gitmodules")); !os.IsNotExist(statErr) {
		t.Fatalf(".gitmodules remains: %v", statErr)
	}
	if got := git(t, root, "ls-files", "-s", "--", "deps/library"); got != "" {
		t.Fatalf("gitlink remains: %q", got)
	}
	if _, statErr := os.Lstat(filepath.Join(root, ".git", "modules")); statErr == nil {
		entries, readErr := os.ReadDir(filepath.Join(root, ".git", "modules"))
		if readErr != nil || len(entries) != 0 {
			t.Fatalf("module store residue: entries=%v err=%v", entries, readErr)
		}
	}
	change, err = a.Remove(context.Background(), root, dep, exp, next)
	if err != nil || change.Changed {
		t.Fatalf("second Unwire() = %#v, %v", change, err)
	}
}

func TestPullRecreatesMissingCheckout(t *testing.T) {
	fixture := newFixture(t)
	root := initRepository(t)
	a := Adapter{}
	dep := adapter.Dependency{Name: "library", Git: fixture.url}
	exp := adapter.Export{Adapter: "submodule", Name: "library"}
	locked := adapter.Locked{Git: fixture.url, Commit: fixture.first}
	if _, err := a.Pull(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "deps", "library")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Pull(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatal(err)
	}
	assertExactState(t, root, "deps/library", fixture.url, fixture.first)
}

func TestRemovePreservesUnrelatedSubmoduleRegistration(t *testing.T) {
	target := newFixture(t)
	unrelated := newFixture(t)
	root := initRepository(t)
	a := Adapter{}
	targetDep := adapter.Dependency{Name: "target", Git: target.url}
	targetExport := adapter.Export{Adapter: "submodule", Name: "target", Path: "deps/target"}
	targetLock := adapter.Locked{Git: target.url, Commit: target.first}
	unrelatedDep := adapter.Dependency{Name: "unrelated", Git: unrelated.url}
	unrelatedExport := adapter.Export{Adapter: "submodule", Name: "unrelated", Path: "deps/unrelated"}
	unrelatedLock := adapter.Locked{Git: unrelated.url, Commit: unrelated.first}

	if _, err := a.Pull(context.Background(), root, targetDep, targetExport, targetLock); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Pull(context.Background(), root, unrelatedDep, unrelatedExport, unrelatedLock); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Remove(context.Background(), root, targetDep, targetExport, targetLock); err != nil {
		t.Fatal(err)
	}

	if got := git(t, root, "ls-files", "-s", "--", "deps/target"); got != "" {
		t.Fatalf("target gitlink remains: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(root, ".git", "modules", "deps", "target")); !os.IsNotExist(err) {
		t.Fatalf("target module store remains: %v", err)
	}
	assertExactState(t, root, "deps/unrelated", unrelated.url, unrelated.first)
	modules, err := os.ReadFile(filepath.Join(root, ".gitmodules"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(modules), "deps/target") || !strings.Contains(string(modules), "deps/unrelated") {
		t.Fatalf("unexpected .gitmodules after targeted removal:\n%s", modules)
	}
}

func TestWireRefusesExistingUnownedPathAndUnsafePath(t *testing.T) {
	fixture := newFixture(t)
	root := initRepository(t)
	dep := adapter.Dependency{Name: "library", Git: fixture.url}
	locked := adapter.Locked{Git: fixture.url, Commit: fixture.first}
	if err := os.MkdirAll(filepath.Join(root, "deps", "library"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := (Adapter{}).Wire(context.Background(), root, dep, adapter.Export{Path: "deps/library"}, locked); err == nil || !strings.Contains(err.Error(), "not owned") {
		t.Fatalf("collision error = %v", err)
	}
	if _, err := (Adapter{}).Wire(context.Background(), root, dep, adapter.Export{Path: "../library"}, locked); err == nil || !strings.Contains(err.Error(), "..-free") {
		t.Fatalf("unsafe path error = %v", err)
	}
}

func TestWireRefusesUnmergedIndexEntry(t *testing.T) {
	fixture := newFixture(t)
	root := initRepository(t)
	one := gitInput(t, root, "one\n", "hash-object", "-w", "--stdin")
	two := gitInput(t, root, "two\n", "hash-object", "-w", "--stdin")
	index := "100644 " + one + " 2\tdeps/library\n100644 " + two + " 3\tdeps/library\n"
	gitInput(t, root, index, "update-index", "--index-info")
	dep := adapter.Dependency{Name: "library", Git: fixture.url}
	locked := adapter.Locked{Git: fixture.url, Commit: fixture.first}
	if _, err := (Adapter{}).Wire(context.Background(), root, dep, adapter.Export{}, locked); err == nil || !strings.Contains(err.Error(), "unresolved index conflicts") {
		t.Fatalf("conflict error = %v", err)
	}
}

func TestWireRefusesRegisteredSourceMismatch(t *testing.T) {
	first := newFixture(t)
	second := newFixture(t)
	root := initRepository(t)
	a := Adapter{}
	exp := adapter.Export{Path: "deps/library"}
	dep := adapter.Dependency{Name: "library", Git: first.url}
	if _, err := a.Wire(context.Background(), root, dep, exp, adapter.Locked{Git: first.url, Commit: first.first}); err != nil {
		t.Fatal(err)
	}
	dep.Git = second.url
	if _, err := a.Wire(context.Background(), root, dep, exp, adapter.Locked{Git: second.url, Commit: second.first}); err == nil || !strings.Contains(err.Error(), "belongs to") {
		t.Fatalf("source mismatch error = %v", err)
	}
}

func TestFailedAddLeavesNoRegistrationOrGitResidue(t *testing.T) {
	for _, source := range []string{"file:///definitely/not/a/repository", newFixture(t).url} {
		root := initRepository(t)
		dep := adapter.Dependency{Name: "missing", Git: source}
		locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
		if _, err := (Adapter{}).Wire(context.Background(), root, dep, adapter.Export{}, locked); err == nil {
			t.Fatal("Wire() unexpectedly succeeded")
		}
		for _, path := range []string{".gitmodules", "deps/missing", ".git/modules/deps/missing"} {
			if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path))); !os.IsNotExist(err) {
				t.Fatalf("failed add from %s left %s: %v", source, path, err)
			}
		}
		if got := git(t, root, "ls-files", "-s", "--", "deps/missing"); got != "" {
			t.Fatalf("failed add from %s left gitlink %q", source, got)
		}
	}
}

type gitFixture struct {
	t      *testing.T
	source string
	remote string
	url    string
	first  string
}

func newFixture(t *testing.T) *gitFixture {
	t.Helper()
	source := filepath.Join(t.TempDir(), "source")
	remote := filepath.Join(t.TempDir(), "remote.git")
	mustRun(t, "", "git", "init", "-b", "main", source)
	configureIdentity(t, source)
	if err := os.WriteFile(filepath.Join(source, "content.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, source, "add", "content.txt")
	git(t, source, "commit", "-m", "one")
	first := git(t, source, "rev-parse", "HEAD")
	mustRun(t, "", "git", "clone", "--bare", source, remote)
	git(t, source, "remote", "add", "origin", remote)
	return &gitFixture{t: t, source: source, remote: remote, url: fileURL(remote), first: first}
}

func (f *gitFixture) commit(t *testing.T, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.source, "content.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, f.source, "commit", "-am", "next")
	git(t, f.source, "push", "origin", "main")
	return git(t, f.source, "rev-parse", "HEAD")
}

func initRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustRun(t, "", "git", "init", "-b", "main", root)
	configureIdentity(t, root)
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("consumer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "README.md")
	git(t, root, "commit", "-m", "initial")
	return root
}

func configureIdentity(t *testing.T, root string) {
	t.Helper()
	git(t, root, "config", "user.name", "git-a2a test")
	git(t, root, "config", "user.email", "git-a2a@example.invalid")
}

func assertExactState(t *testing.T, root, relative, source, commit string) {
	t.Helper()
	entry := strings.Fields(git(t, root, "ls-files", "-s", "--", relative))
	if len(entry) < 2 || entry[0] != "160000" || entry[1] != commit {
		t.Fatalf("gitlink = %v, want 160000 %s", entry, commit)
	}
	dest := filepath.Join(root, filepath.FromSlash(relative))
	if got := git(t, dest, "rev-parse", "HEAD"); got != commit {
		t.Fatalf("checkout = %s, want %s", got, commit)
	}
	if got := git(t, dest, "rev-parse", "--abbrev-ref", "HEAD"); got != "HEAD" {
		t.Fatalf("checkout is attached to %s", got)
	}
	modules, err := os.ReadFile(filepath.Join(root, ".gitmodules"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(modules), "path = "+relative) || !strings.Contains(string(modules), "url = "+source) {
		t.Fatalf("unexpected .gitmodules:\n%s", modules)
	}
}

func fileURL(path string) string {
	path = filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		return "file:///" + strings.TrimPrefix(path, "/")
	}
	return "file://" + path
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return strings.TrimSpace(mustRun(t, dir, "git", args...))
}

func mustRun(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

func gitInput(t *testing.T, dir, input string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
