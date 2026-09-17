package zig

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/adapter"
)

func TestWireGoldenIdempotentUnwire(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join("..", "..", "testdata", "consumer-zig")
	original := mustRead(t, filepath.Join(fixture, "build.zig.zon"))
	if err := os.WriteFile(filepath.Join(root, "build.zig.zon"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{Name: "acme-lib-utils", Git: "https://github.com/acme/lib-utils.git", Ref: "main"}
	exp := adapter.Export{Adapter: "zig", Name: "acme_lib_utils", Checksum: "1220" + strings.Repeat("b", 64)}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	a := Adapter{}
	change, err := a.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed {
		t.Fatalf("wire=%#v err=%v", change, err)
	}
	if got, want := mustRead(t, filepath.Join(root, "build.zig.zon")), mustRead(t, filepath.Join(fixture, "build.golden.zig.zon")); string(got) != string(want) {
		t.Fatalf("golden differs\ngot:\n%s\nwant:\n%s", got, want)
	}
	if change, err = a.Wire(context.Background(), root, dep, exp, locked); err != nil || change.Changed {
		t.Fatalf("second wire=%#v err=%v", change, err)
	}
	if findings, err := a.Drift(context.Background(), root, dep, exp, locked); err != nil || len(findings) != 0 {
		t.Fatalf("drift=%v err=%v", findings, err)
	}
	if change, err = a.Unwire(context.Background(), root, dep, exp); err != nil || !change.Changed {
		t.Fatalf("unwire=%#v err=%v", change, err)
	}
	if got := mustRead(t, filepath.Join(root, "build.zig.zon")); string(got) != string(original) {
		t.Fatalf("unwire differs\ngot:\n%s\nwant:\n%s", got, original)
	}
}

func TestMissingHashIsNotWirable(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "build.zig.zon"), []byte(".{ .dependencies = .{} }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "build.zig.zon")
	original := mustRead(t, path)
	err := (Adapter{}).Capability(root, adapter.Dependency{}, adapter.Export{Name: "dep"})
	if !adapter.IsNotWirable(err) {
		t.Fatalf("err=%v, want typed not-wirable", err)
	}
	if got := mustRead(t, path); string(got) != string(original) {
		t.Fatal("Capability mutated build.zig.zon")
	}
}

func TestPullChecksToolBeforeMutation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "build.zig.zon")
	original := []byte(".{ .dependencies = .{} }\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	dep := adapter.Dependency{Git: "https://example.com/acme/lib.git"}
	exp := adapter.Export{Name: "dep", Checksum: "1220" + strings.Repeat("b", 64)}
	_, err := (Adapter{}).Pull(context.Background(), root, dep, exp, adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)})
	if !adapter.IsMissingTool(err) {
		t.Fatalf("err=%v, want missing-tool", err)
	}
	if got := mustRead(t, path); string(got) != string(original) {
		t.Fatal("Pull mutated build.zig.zon before tool preflight")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
