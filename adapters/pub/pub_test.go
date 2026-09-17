package pub

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
	fixture := filepath.Join("..", "..", "testdata", "consumer-pub")
	original, _ := os.ReadFile(filepath.Join(fixture, "pubspec.yaml"))
	_ = os.WriteFile(filepath.Join(root, "pubspec.yaml"), original, 0o644)
	dep := adapter.Dependency{Name: "acme-lib", Git: "https://github.com/acme/lib-utils.git", Ref: "main"}
	exp := adapter.Export{Adapter: "pub", Name: "acme_lib_utils", Path: "dart/package"}
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
	manifestPath := filepath.Join(root, "pubspec.yaml")
	branchPin := strings.Replace(string(mustRead(t, manifestPath)), `ref: "`+locked.Commit+`"`, `ref: "main"`, 1)
	if err := os.WriteFile(manifestPath, []byte(branchPin), 0o644); err != nil {
		t.Fatal(err)
	}
	if findings, err := a.Drift(context.Background(), root, dep, exp, locked); err != nil || len(findings) != 1 {
		t.Fatalf("branch pin drift=%v err=%v", findings, err)
	}
	if _, err := a.Wire(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatal(err)
	}
	if change, err = a.Unwire(context.Background(), root, dep, exp); err != nil || !change.Changed {
		t.Fatalf("unwire=%#v err=%v", change, err)
	}
	got, _ = os.ReadFile(filepath.Join(root, "pubspec.yaml"))
	if string(got) != string(original) {
		t.Fatalf("unwire differs\ngot:\n%s\nwant:\n%s", got, original)
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
