package golang

import (
	"context"
	"github.com/neprel/git-a2a/internal/adapter"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWireGoldenIdempotentUnwire(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join("..", "..", "testdata", "consumer-go")
	original, err := os.ReadFile(filepath.Join(fixture, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "go.mod"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{Git: "https://github.com/acme/lib-utils.git", Ref: "main", Track: "locked"}
	exp := adapter.Export{Ecosystem: "golang", Name: "acme.dev/lib-utils"}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	a := Adapter{}
	change, err := a.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed {
		t.Fatalf("wire: %#v %v", change, err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "go.mod"))
	want, _ := os.ReadFile(filepath.Join(fixture, "go.golden.mod"))
	if string(got) != string(want) {
		t.Fatalf("golden differs\ngot:\n%s\nwant:\n%s", got, want)
	}
	change, err = a.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || change.Changed {
		t.Fatalf("second wire: %#v %v", change, err)
	}
	if findings, err := a.Drift(context.Background(), root, dep, exp, locked); err != nil || len(findings) != 0 {
		t.Fatalf("clean drift: %v %v", findings, err)
	}
	wrong := locked
	wrong.Git = "https://github.com/acme/fork.git"
	if findings, err := a.Drift(context.Background(), root, dep, exp, wrong); err != nil || len(findings) != 1 {
		t.Fatalf("source drift: %v %v", findings, err)
	}
	change, err = a.Unwire(context.Background(), root, dep, exp)
	if err != nil || !change.Changed {
		t.Fatalf("unwire: %#v %v", change, err)
	}
	got, _ = os.ReadFile(filepath.Join(root, "go.mod"))
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(original)) {
		t.Fatalf("unwire differs\ngot:\n%s\nwant:\n%s", got, original)
	}
}

func TestDriftMissingEntryIsUnwired(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module acme.dev/consumer\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err := (Adapter{}).Drift(context.Background(), root,
		adapter.Dependency{Git: "https://github.com/acme/lib.git"},
		adapter.Export{Ecosystem: "golang", Name: "acme.dev/lib"},
		adapter.Locked{Git: "https://github.com/acme/lib.git", Commit: strings.Repeat("a", 40)})
	if err != nil || len(findings) != 1 {
		t.Fatalf("findings=%v err=%v", findings, err)
	}
}
