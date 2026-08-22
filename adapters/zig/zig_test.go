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
	dep := adapter.Dependency{Git: "https://github.com/acme/lib-utils.git", Ref: "main", Track: "locked"}
	exp := adapter.Export{Ecosystem: "zig", Name: "acme_lib_utils", Extensions: map[string]any{"x-zig-hash": "1220" + strings.Repeat("b", 64)}}
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
	_, err := (Adapter{}).Wire(context.Background(), root, adapter.Dependency{}, adapter.Export{Name: "dep"}, adapter.Locked{})
	if !adapter.IsNotWirable(err) {
		t.Fatalf("err=%v, want typed not-wirable", err)
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
