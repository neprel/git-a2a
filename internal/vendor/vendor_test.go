package vendor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/neprel/git-a2a/internal/gitx"
	"github.com/neprel/git-a2a/internal/manifest"
)

func TestSubmoduleRoundTripDriftAndResidueCleanup(t *testing.T) {
	consumer, url, commit := repositories(t)
	manager := Manager{Runner: gitx.ExecRunner{Timeout: 30 * time.Second}}
	dep := manifest.Dependency{ID: "acme-lib", Git: url, Ref: "main", Track: "locked", Vendor: &manifest.Vendor{Mode: "submodule"}}
	own := &manifest.Manifest{Schema: 1, Module: manifest.Module{ID: "consumer"}}
	locked := manifest.LockedDependency{Git: url, Ref: "main", Path: ".", Commit: commit}

	vendorLock, err := manager.Apply(context.Background(), consumer, own, dep, locked, false)
	if err != nil {
		t.Fatal(err)
	}
	locked.Vendor = vendorLock
	index := git(t, consumer, "ls-files", "-s", "--", "deps/acme-lib")
	if !strings.HasPrefix(index, "160000 "+commit+" ") {
		t.Fatalf("gitlink = %q", index)
	}
	state, err := manager.Inspect(context.Background(), consumer, dep, locked)
	if err != nil || len(state.Findings) != 0 || !strings.HasPrefix(state.Label, "submodule @") {
		t.Fatalf("state = %#v, err = %v", state, err)
	}
	if err = os.WriteFile(filepath.Join(consumer, "deps", "acme-lib", "README.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err = manager.Inspect(context.Background(), consumer, dep, locked)
	if err != nil || !hasFinding(state, "dirty") {
		t.Fatalf("dirty state = %#v, err = %v", state, err)
	}
	if err = manager.Remove(context.Background(), consumer, dep, locked, false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("remove dirty error = %v", err)
	}
	git(t, filepath.Join(consumer, "deps", "acme-lib"), "checkout", "--", ".")
	if err = manager.Remove(context.Background(), consumer, dep, locked, false); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(consumer, ".gitmodules")); !os.IsNotExist(err) {
		t.Fatalf(".gitmodules remains: %v", err)
	}
	if _, err = os.Stat(filepath.Join(consumer, ".git", "modules", "deps", "acme-lib")); !os.IsNotExist(err) {
		t.Fatalf(".git/modules residue remains: %v", err)
	}
}

func TestCopyUsesIndexTreeExcludingStampAndPreservesEntries(t *testing.T) {
	consumer, url, commit := repositories(t)
	manager := Manager{Runner: gitx.ExecRunner{Timeout: 30 * time.Second}}
	dep := manifest.Dependency{ID: "acme-lib", Git: url, Ref: "main", Track: "locked", Vendor: &manifest.Vendor{Mode: "copy"}}
	own := &manifest.Manifest{Schema: 1, Module: manifest.Module{ID: "consumer"}}
	locked := manifest.LockedDependency{Git: url, Ref: "main", Path: ".", Commit: commit}

	vendorLock, err := manager.Apply(context.Background(), consumer, own, dep, locked, false)
	if err != nil {
		t.Fatal(err)
	}
	locked.Vendor = vendorLock
	if vendorLock.Tree == "" {
		t.Fatal("copy lock has no tree")
	}
	stamp, err := os.ReadFile(filepath.Join(consumer, "deps", "acme-lib", StampName))
	if err != nil || !strings.Contains(string(stamp), commit) {
		t.Fatalf("stamp = %q, err = %v", stamp, err)
	}
	if runtime.GOOS != "windows" {
		info, statErr := os.Lstat(filepath.Join(consumer, "deps", "acme-lib", "README.link"))
		if statErr != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("symlink not preserved: %v, %v", info, statErr)
		}
	}
	state, err := manager.Inspect(context.Background(), consumer, dep, locked)
	if err != nil || len(state.Findings) != 0 || state.Label != "copy" {
		t.Fatalf("state = %#v, err = %v", state, err)
	}
	readme := filepath.Join(consumer, "deps", "acme-lib", "README.md")
	if err = os.WriteFile(readme, []byte("worktree-only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err = manager.Inspect(context.Background(), consumer, dep, locked)
	if err != nil || len(state.Findings) != 0 {
		t.Fatalf("unstaged worktree must not change index provenance: %#v, %v", state, err)
	}
	git(t, consumer, "add", "--", "deps/acme-lib/README.md")
	state, err = manager.Inspect(context.Background(), consumer, dep, locked)
	if err != nil || !hasFinding(state, "tree") {
		t.Fatalf("staged drift = %#v, err = %v", state, err)
	}
	if err = manager.Remove(context.Background(), consumer, dep, locked, true); err != nil {
		t.Fatal(err)
	}
}

func TestValidatePathRejectsSymlinkComponent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privilege is not guaranteed")
	}
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "deps")); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePath(root, "deps/acme-lib"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("error = %v", err)
	}
}

func TestFailedSubmoduleAddRollsBackGitmodulesAndModuleStore(t *testing.T) {
	consumer, url, commit := repositories(t)
	delegate := gitx.ExecRunner{Timeout: 30 * time.Second}
	manager := Manager{Runner: failGitRunner{delegate: delegate, verb: "checkout"}}
	dep := manifest.Dependency{ID: "acme-lib", Git: url, Ref: "main", Track: "locked", Vendor: &manifest.Vendor{Mode: "submodule"}}
	own := &manifest.Manifest{Schema: 1, Module: manifest.Module{ID: "consumer"}}
	locked := manifest.LockedDependency{Git: url, Ref: "main", Path: ".", Commit: commit}
	if _, err := manager.Apply(context.Background(), consumer, own, dep, locked, false); err == nil || !strings.Contains(err.Error(), "invalid argument") {
		t.Fatalf("error = %v", err)
	}
	for _, path := range []string{
		filepath.Join(consumer, ".gitmodules"),
		filepath.Join(consumer, "deps", "acme-lib"),
		filepath.Join(consumer, ".git", "modules", "deps", "acme-lib"),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("rollback residue %s: %v", path, err)
		}
	}
	if got := strings.TrimSpace(git(t, consumer, "ls-files", "-s", "--", "deps/acme-lib")); got != "" {
		t.Fatalf("rollback left gitlink: %q", got)
	}
}

func repositories(t *testing.T) (consumer, url, commit string) {
	t.Helper()
	base := t.TempDir()
	source := filepath.Join(base, "source")
	consumer = filepath.Join(base, "consumer")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, source, "init", "-b", "main")
	git(t, source, "config", "user.email", "acme@example.test")
	git(t, source, "config", "user.name", "Acme Test")
	if err := os.WriteFile(filepath.Join(source, "a2amodule.yml"), []byte("schema: 1\nmodule: {id: acme-lib}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("acme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink("README.md", filepath.Join(source, "README.link")); err != nil {
			t.Fatal(err)
		}
	}
	git(t, source, "add", ".")
	git(t, source, "commit", "-m", "feat: initial acme library")
	commit = strings.TrimSpace(git(t, source, "rev-parse", "HEAD"))
	bare := filepath.Join(base, "acme-lib.git")
	git(t, base, "clone", "--bare", source, bare)
	url = "file://" + filepath.ToSlash(bare)

	if err := os.MkdirAll(consumer, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, consumer, "init", "-b", "main")
	git(t, consumer, "config", "user.email", "consumer@example.test")
	git(t, consumer, "config", "user.name", "Consumer Test")
	if err := os.WriteFile(filepath.Join(consumer, "a2amodule.yml"), []byte("schema: 1\nmodule: {id: consumer}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, consumer, "add", ".")
	git(t, consumer, "commit", "-m", "chore: initialize consumer")
	return consumer, url, commit
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func hasFinding(state State, kind string) bool {
	for _, finding := range state.Findings {
		if finding.Kind == kind {
			return true
		}
	}
	return false
}

type failGitRunner struct {
	delegate gitx.Runner
	verb     string
}

func (runner failGitRunner) Run(ctx context.Context, dir string, stdin []byte, args ...string) ([]byte, error) {
	for _, arg := range args {
		if arg == runner.verb {
			return nil, os.ErrInvalid
		}
	}
	return runner.delegate.Run(ctx, dir, stdin, args...)
}
