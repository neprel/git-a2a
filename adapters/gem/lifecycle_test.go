package gem

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/adapter"
)

func TestLifecycleAlwaysInstallsAndInspectsLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-backed fake tool")
	}
	root := t.TempDir()
	copyFile(t, filepath.Join("..", "..", "testdata", "consumer-gem", "Gemfile"), filepath.Join(root, "Gemfile"))
	commit := strings.Repeat("a", 40)
	log := installFakeTool(t, "bundle", `printf '%s\n' "$*" >> "$GITA2A_TOOL_LOG"
if [ "$1" = "--version" ]; then echo 'Bundler version 2.5.0'; exit 0; fi
cat > Gemfile.lock <<EOF
GIT
  remote: $GITA2A_GIT
  revision: $GITA2A_COMMIT
  specs:
    lib (1.0.0)

GEM
  specs:
EOF`)
	t.Setenv("GITA2A_COMMIT", commit)
	dep, exp := adapter.Dependency{Git: "https://example.test/lib.git"}, adapter.Export{Name: "lib"}
	t.Setenv("GITA2A_GIT", dep.Git)
	locked := adapter.Locked{Git: dep.Git, Commit: commit}
	a := Adapter{}
	if err := a.Capability(root, dep, exp); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Pull(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "Gemfile.lock")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Pull(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatal(err)
	}
	if findings, err := a.Inspect(context.Background(), root, dep, exp, locked); err != nil || len(findings) != 0 {
		t.Fatalf("findings=%v err=%v", findings, err)
	}
	if _, err := a.Remove(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(mustRead(t, log)), "install --quiet\n"); got != 3 {
		t.Fatalf("install count=%d", got)
	}
}

func TestInspectRejectsCommitFromUnrelatedBundlerGitSource(t *testing.T) {
	root := t.TempDir()
	copyFile(t, filepath.Join("..", "..", "testdata", "consumer-gem", "Gemfile"), filepath.Join(root, "Gemfile"))
	dep, exp := adapter.Dependency{Git: "https://example.test/lib.git"}, adapter.Export{Name: "lib"}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	if _, err := (Adapter{}).Wire(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatal(err)
	}
	body := "GIT\n  remote: https://example.test/other.git\n  revision: " + locked.Commit + "\n  specs:\n    other (1.0.0)\n\nGIT\n  remote: " + dep.Git + "\n  revision: " + strings.Repeat("b", 40) + "\n  specs:\n    lib (1.0.0)\n"
	if err := os.WriteFile(filepath.Join(root, "Gemfile.lock"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err := (Adapter{}).Inspect(context.Background(), root, dep, exp, locked)
	if err != nil || len(findings) != 1 || findings[0].File != "Gemfile.lock" {
		t.Fatalf("findings=%v err=%v", findings, err)
	}
}

func installFakeTool(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "calls.log")
	t.Setenv("GITA2A_TOOL_LOG", log)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}
func copyFile(t *testing.T, from, to string) {
	t.Helper()
	if err := os.WriteFile(to, mustRead(t, from), 0o644); err != nil {
		t.Fatal(err)
	}
}
