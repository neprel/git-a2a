package clojure

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
	fixture := filepath.Join("..", "..", "testdata", "consumer-clojure")
	original := mustRead(t, filepath.Join(fixture, "deps.edn"))
	if err := os.WriteFile(filepath.Join(root, "deps.edn"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{Name: "acme-lib-utils", Git: "https://github.com/acme/lib-utils.git", Ref: "main"}
	exp := adapter.Export{Adapter: "clojure", Name: "acme/lib-utils", Path: "clojure/lib"}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	a := Adapter{}
	change, err := a.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed {
		t.Fatalf("wire=%#v err=%v", change, err)
	}
	if got, want := mustRead(t, filepath.Join(root, "deps.edn")), mustRead(t, filepath.Join(fixture, "deps.golden.edn")); string(got) != string(want) {
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
	if got := mustRead(t, filepath.Join(root, "deps.edn")); string(got) != string(original) {
		t.Fatalf("unwire differs\ngot:\n%s\nwant:\n%s", got, original)
	}
}

func TestCapabilityRejectsUnqualifiedLibWithoutMutation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "deps.edn")
	original := []byte("{:deps {}}\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	err := (Adapter{}).Capability(root, adapter.Dependency{}, adapter.Export{Name: "unqualified"})
	if !adapter.IsNotWirable(err) {
		t.Fatalf("err=%v, want typed not-wirable", err)
	}
	if got := mustRead(t, path); string(got) != string(original) {
		t.Fatal("Capability mutated deps.edn")
	}
}

func TestPullChecksToolBeforeMutation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "deps.edn")
	original := []byte("{:deps {}}\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	dep := adapter.Dependency{Git: "https://example.com/acme/lib.git"}
	exp := adapter.Export{Name: "acme/lib"}
	_, err := (Adapter{}).Pull(context.Background(), root, dep, exp, adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)})
	if !adapter.IsMissingTool(err) {
		t.Fatalf("err=%v, want missing-tool", err)
	}
	if got := mustRead(t, path); string(got) != string(original) {
		t.Fatal("Pull mutated deps.edn before tool preflight")
	}
}

func TestInspectReportsMissingGitlibsCheckoutAsRepairable(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "deps.edn"), []byte("{:deps {}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	dep := adapter.Dependency{Git: "https://example.com/acme/lib.git"}
	exp := adapter.Export{Name: "acme/lib"}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	a := Adapter{}
	if _, err := a.Wire(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatal(err)
	}
	findings, err := a.Inspect(context.Background(), root, dep, exp, locked)
	if err != nil || len(findings) != 1 || !findings[0].Repairable {
		t.Fatalf("findings=%#v err=%v", findings, err)
	}
	checkout := filepath.Join(home, ".gitlibs", "libs", "acme", "lib", locked.Commit)
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	if findings, err = a.Inspect(context.Background(), root, dep, exp, locked); err != nil || len(findings) != 0 {
		t.Fatalf("findings=%#v err=%v", findings, err)
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
