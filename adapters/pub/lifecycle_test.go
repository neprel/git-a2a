package pub

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/v2/internal/adapter"
)

func TestLifecycleAlwaysGetsAndInspectsMaterialization(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-backed fake tool")
	}
	root := t.TempDir()
	copyFile(t, filepath.Join("..", "..", "testdata", "consumer-pub", "pubspec.yaml"), filepath.Join(root, "pubspec.yaml"))
	log := installFakeTool(t, "dart", `printf '%s\n' "$*" >> "$GITA2A_TOOL_LOG"
if [ "$1" = "--version" ]; then echo 'Dart SDK version: 3.5.0'; exit 0; fi
mkdir -p .dart_tool/materialized
cat > pubspec.lock <<'EOF'
packages:
  lib:
    dependency: "direct main"
    description:
      resolved-ref: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    source: git
EOF
printf '{"packages":[{"name":"lib","rootUri":"materialized"}]}\n' > .dart_tool/package_config.json`)
	dep, exp := adapter.Dependency{Git: "https://example.test/lib.git"}, adapter.Export{Name: "lib"}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	a := Adapter{}
	if err := a.Capability(root, dep, exp); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Pull(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, ".dart_tool")); err != nil {
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
	if got := strings.Count(string(mustRead(t, log)), "pub get\n"); got != 3 {
		t.Fatalf("pub get count=%d", got)
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
