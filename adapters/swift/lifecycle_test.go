package swift

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/adapter"
)

func TestLifecycleAlwaysResolvesAndInspectsLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-backed fake tool")
	}
	root := t.TempDir()
	copyFile(t, filepath.Join("..", "..", "testdata", "consumer-swift", "Package.swift"), filepath.Join(root, "Package.swift"))
	log := installFakeTool(t, "swift", `printf '%s\n' "$*" >> "$GITA2A_TOOL_LOG"
if [ "$1" = "--version" ]; then echo 'Swift version 6.0'; exit 0; fi
cat > Package.resolved <<EOF
{"version":2,"pins":[{"identity":"lib","kind":"remoteSourceControl","location":"$GITA2A_GIT","state":{"revision":"$GITA2A_COMMIT","version":null}}]}
EOF`)
	dep, exp := adapter.Dependency{Git: "https://example.test/lib.git"}, adapter.Export{Name: "Lib"}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	t.Setenv("GITA2A_GIT", dep.Git)
	t.Setenv("GITA2A_COMMIT", locked.Commit)
	a := Adapter{}
	if err := a.Capability(root, dep, exp); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Pull(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "Package.resolved")); err != nil {
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
	if got := strings.Count(string(mustRead(t, log)), "package resolve\n"); got != 3 {
		t.Fatalf("resolve count=%d", got)
	}
}

func TestInspectRejectsUnrelatedSwiftPinAtExpectedRevision(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "Package.swift")
	copyFile(t, filepath.Join("..", "..", "testdata", "consumer-swift", "Package.swift"), target)
	dep, exp := adapter.Dependency{Git: "https://example.test/lib.git"}, adapter.Export{Name: "Lib"}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	if _, err := (Adapter{}).Wire(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatal(err)
	}
	body := `{"version":2,"pins":[{"identity":"other","kind":"remoteSourceControl","location":"https://example.test/other.git","state":{"revision":"` + locked.Commit + `"}}]}`
	if err := os.WriteFile(filepath.Join(root, "Package.resolved"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err := (Adapter{}).Inspect(context.Background(), root, dep, exp, locked)
	if err != nil || len(findings) != 1 || findings[0].File != "Package.resolved" {
		t.Fatalf("findings=%v err=%v", findings, err)
	}
}

func TestCapabilityRejectsSubdirectoryWithoutMutation(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "Package.swift")
	copyFile(t, filepath.Join("..", "..", "testdata", "consumer-swift", "Package.swift"), target)
	before := mustRead(t, target)
	err := (Adapter{}).Capability(root, adapter.Dependency{Git: "https://example.test/lib.git"}, adapter.Export{Name: "Lib", Path: "swift/lib"})
	if !adapter.IsNotWirable(err) {
		t.Fatalf("Capability error=%v, want NotWirable", err)
	}
	if after := mustRead(t, target); string(after) != string(before) {
		t.Fatal("Capability mutated Package.swift")
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
