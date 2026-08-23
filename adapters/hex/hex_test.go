package hex

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
	fixture := filepath.Join("..", "..", "testdata", "consumer-hex")
	original := mustRead(t, filepath.Join(fixture, "mix.exs"))
	if err := os.WriteFile(filepath.Join(root, "mix.exs"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{Git: "https://github.com/acme/lib-utils.git", Ref: "main", Track: "locked"}
	exp := adapter.Export{Ecosystem: "hex", Name: "acme_lib_utils", Path: "elixir/lib"}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	a := Adapter{}
	change, err := a.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed {
		t.Fatalf("wire=%#v err=%v", change, err)
	}
	if got, want := mustRead(t, filepath.Join(root, "mix.exs")), mustRead(t, filepath.Join(fixture, "mix.golden.exs")); string(got) != string(want) {
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
	if got := mustRead(t, filepath.Join(root, "mix.exs")); string(got) != string(original) {
		t.Fatalf("unwire differs\ngot:\n%s\nwant:\n%s", got, original)
	}
}

func TestVendoredPathLifecycle(t *testing.T) {
	root := t.TempDir()
	original := "defmodule Consumer.MixProject do\n  use Mix.Project\n  def project, do: [app: :consumer, version: \"1.0.0\", deps: deps()]\n  defp deps do\n    []\n  end\nend\n"
	if err := os.WriteFile(filepath.Join(root, "mix.exs"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{ID: "acme-lib", Vendor: &manifest.Vendor{Mode: "copy"}}
	exp := adapter.Export{Ecosystem: "hex", Name: "acme_lib", Path: "elixir"}
	locked := adapter.Locked{Vendor: &manifest.LockedVendor{Mode: "copy", Path: "deps/acme-lib"}}
	a := Adapter{}
	if change, err := a.Wire(context.Background(), root, dep, exp, locked); err != nil || !change.Changed {
		t.Fatalf("Wire=%#v %v", change, err)
	}
	if got := string(mustRead(t, filepath.Join(root, "mix.exs"))); !strings.Contains(got, `path: "deps/acme-lib/elixir"`) {
		t.Fatalf("path wiring:\n%s", got)
	}
	if findings, err := a.Drift(context.Background(), root, dep, exp, locked); err != nil || len(findings) != 0 {
		t.Fatalf("Drift=%v %v", findings, err)
	}
	if change, err := a.Unwire(context.Background(), root, dep, exp); err != nil || !change.Changed {
		t.Fatalf("Unwire=%#v %v", change, err)
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
