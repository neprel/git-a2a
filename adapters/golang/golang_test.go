package golang

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

func TestWireFloatingUsesLockedPseudoVersionAndDoesNotDuplicateRequireBlock(t *testing.T) {
	root := t.TempDir()
	original := "module acme.dev/consumer\n\ngo 1.24\n\nrequire (\n\tacme.dev/lib v0.0.0\n\tacme.dev/other v1.2.3\n)\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("b", 40)
	dep := adapter.Dependency{Git: "https://github.com/acme/lib.git", Ref: "main", Track: "floating"}
	exp := adapter.Export{Ecosystem: "golang", Name: "acme.dev/lib"}
	if _, err := (Adapter{}).Wire(context.Background(), root, dep, exp, adapter.Locked{Git: dep.Git, Commit: commit}); err != nil {
		t.Fatal(err)
	}
	got := string(mustReadFile(t, filepath.Join(root, "go.mod")))
	if strings.Count(got, "acme.dev/lib v0.0.0") != 1 {
		t.Fatalf("require duplicated:\n%s", got)
	}
	if !strings.Contains(got, "github.com/acme/lib v0.0.0-00010101000000-bbbbbbbbbbbb") || strings.Contains(got, " main") {
		t.Fatalf("floating dependency was not pinned to locked commit:\n%s", got)
	}
	if findings, err := (Adapter{}).Drift(context.Background(), root, dep, exp, adapter.Locked{Git: dep.Git, Commit: commit}); err != nil || len(findings) != 0 {
		t.Fatalf("drift=%v err=%v", findings, err)
	}
}

func TestUnwirePreservesPrecedingBlankLines(t *testing.T) {
	root := t.TempDir()
	original := "module acme.dev/consumer\n\ngo 1.24\n\nrequire acme.dev/lib v0.0.0\n\nreplace acme.dev/lib => github.com/acme/lib v0.0.0-00010101000000-aaaaaaaaaaaa\n\nexclude acme.dev/other v1.0.0\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Adapter{}).Unwire(context.Background(), root, adapter.Dependency{}, adapter.Export{Name: "acme.dev/lib"}); err != nil {
		t.Fatal(err)
	}
	got := string(mustReadFile(t, filepath.Join(root, "go.mod")))
	if !strings.Contains(got, "go 1.24\n\nexclude acme.dev/other v1.0.0") {
		t.Fatalf("surrounding blank line was not preserved:\n%s", got)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
