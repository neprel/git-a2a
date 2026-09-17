package nix

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/v2/internal/adapter"
)

func TestWireGoldenIdempotentUnwire(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join("..", "..", "testdata", "consumer-nix")
	original := mustRead(t, filepath.Join(fixture, "flake.nix"))
	if err := os.WriteFile(filepath.Join(root, "flake.nix"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{Name: "acme-lib-utils", Git: "https://github.com/acme/lib-utils.git", Ref: "main"}
	exp := adapter.Export{Adapter: "nix", Name: "acme-lib-utils", Path: "nix/lib"}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	a := Adapter{}
	change, err := a.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed {
		t.Fatalf("wire=%#v err=%v", change, err)
	}
	if got, want := mustRead(t, filepath.Join(root, "flake.nix")), mustRead(t, filepath.Join(fixture, "flake.golden.nix")); string(got) != string(want) {
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
	if got := mustRead(t, filepath.Join(root, "flake.nix")); string(got) != string(original) {
		t.Fatalf("unwire differs\ngot:\n%s\nwant:\n%s", got, original)
	}
}

func TestPullChecksToolBeforeMutation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "flake.nix")
	original := []byte("{ inputs = {}; outputs = { self }: {}; }\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	dep := adapter.Dependency{Git: "https://example.com/acme/lib.git", Ref: "main"}
	exp := adapter.Export{Name: "dep"}
	_, err := (Adapter{}).Pull(context.Background(), root, dep, exp, adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)})
	if !adapter.IsMissingTool(err) {
		t.Fatalf("err=%v, want missing-tool", err)
	}
	if got := mustRead(t, path); string(got) != string(original) {
		t.Fatal("Pull mutated flake.nix before tool preflight")
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
