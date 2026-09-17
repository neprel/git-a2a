package hex

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/v2/internal/adapter"
)

func TestLifecycleAlwaysGetsAndRemovesThroughMix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-backed fake tool")
	}
	root := t.TempDir()
	copyFile(t, filepath.Join("..", "..", "testdata", "consumer-hex", "mix.exs"), filepath.Join(root, "mix.exs"))
	log := installFakeTool(t, "mix", `printf '%s\n' "$*" >> "$GITA2A_TOOL_LOG"
if [ "$1" = "--version" ]; then echo 'Mix 1.17.0'; exit 0; fi
if [ "$1" = "deps.get" ]; then
  mkdir -p deps/lib/.git
  printf '%s\n' aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa > deps/lib/.git/HEAD
  printf '%s\n' '{"lib": {:git, "https://example.test/lib.git", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", []}}' > mix.lock
fi`)
	dep, exp := adapter.Dependency{Git: "https://example.test/lib.git"}, adapter.Export{Name: "lib"}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	a := Adapter{}
	if err := a.Capability(root, dep, exp); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Pull(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "deps")); err != nil {
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
	calls := string(mustRead(t, log))
	for _, command := range []string{"deps.clean --unused\n", "deps.unlock --unused\n"} {
		if !strings.Contains(calls, command) {
			t.Fatalf("missing %q in %q", command, calls)
		}
	}
	if got := strings.Count(calls, "deps.get\n"); got != 3 {
		t.Fatalf("deps.get count=%d", got)
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
