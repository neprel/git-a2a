package pub

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/adapter"
	"github.com/neprel/git-a2a/internal/manifest"
)

func TestWireGoldenIdempotentUnwire(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join("..", "..", "testdata", "consumer-pub")
	original, _ := os.ReadFile(filepath.Join(fixture, "pubspec.yaml"))
	_ = os.WriteFile(filepath.Join(root, "pubspec.yaml"), original, 0o644)
	dep := adapter.Dependency{Git: "https://github.com/acme/lib-utils.git", Ref: "main", Track: "locked"}
	exp := adapter.Export{Ecosystem: "pub", Name: "acme_lib_utils", Path: "dart/package"}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	a := Adapter{}
	change, err := a.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed {
		t.Fatalf("wire=%#v err=%v", change, err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "pubspec.yaml"))
	want, _ := os.ReadFile(filepath.Join(fixture, "pubspec.golden.yaml"))
	if string(got) != string(want) {
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
	got, _ = os.ReadFile(filepath.Join(root, "pubspec.yaml"))
	if string(got) != string(original) {
		t.Fatalf("unwire differs\ngot:\n%s\nwant:\n%s", got, original)
	}
}

func TestVendoredPathLifecycle(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pubspec.yaml"), []byte("name: consumer\ndependencies:\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{ID: "acme-lib", Vendor: &manifest.Vendor{Mode: "copy"}}
	exp := adapter.Export{Ecosystem: "pub", Name: "acme_lib", Path: "dart"}
	locked := adapter.Locked{Vendor: &manifest.LockedVendor{Mode: "copy", Path: "deps/acme-lib"}}
	a := Adapter{}
	if change, err := a.Wire(context.Background(), root, dep, exp, locked); err != nil || !change.Changed {
		t.Fatalf("Wire=%#v %v", change, err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "pubspec.yaml")); !strings.Contains(string(got), `path: "deps/acme-lib/dart"`) {
		t.Fatalf("path wiring:\n%s", got)
	}
	if findings, err := a.Drift(context.Background(), root, dep, exp, locked); err != nil || len(findings) != 0 {
		t.Fatalf("Drift=%v %v", findings, err)
	}
	if change, err := a.Unwire(context.Background(), root, dep, exp); err != nil || !change.Changed {
		t.Fatalf("Unwire=%#v %v", change, err)
	}
}
