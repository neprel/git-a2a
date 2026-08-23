package msbuild

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/adapter"
	"github.com/neprel/git-a2a/internal/manifest"
)

func TestGoldenRepairAndUnwireLifecycle(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join("..", "..", "testdata", "consumer-msbuild")
	original := mustRead(t, filepath.Join(fixture, "Acme.App.csproj"))
	mustWrite(t, filepath.Join(root, "Acme.App.csproj"), original)
	mustWrite(t, filepath.Join(root, "deps", "acme-lib", "dotnet", "Acme.LibUtils.csproj"), []byte("<Project />\n"))
	implementation := Adapter{}
	dep := adapter.Dependency{ID: "acme-lib", Vendor: &manifest.Vendor{Mode: "copy"}}
	exp := adapter.Export{Ecosystem: "nuget", Name: "Acme.LibUtils", Path: "dotnet"}
	locked := adapter.Locked{Vendor: &manifest.LockedVendor{Mode: "copy", Path: "deps/acme-lib"}}
	change, err := implementation.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed {
		t.Fatalf("Wire = %#v, %v", change, err)
	}
	want := mustRead(t, filepath.Join(fixture, "generated.golden.targets"))
	generated := filepath.Join(root, filepath.FromSlash(generatedFile))
	if got := mustRead(t, generated); !bytes.Equal(got, want) {
		t.Fatalf("golden differs\ngot:\n%s\nwant:\n%s", got, want)
	}
	if change, err = implementation.Wire(context.Background(), root, dep, exp, locked); err != nil || change.Changed {
		t.Fatalf("second Wire = %#v, %v", change, err)
	}
	if findings, driftErr := implementation.Drift(context.Background(), root, dep, exp, locked); driftErr != nil || len(findings) != 0 {
		t.Fatalf("Drift = %#v, %v", findings, driftErr)
	}
	if err = os.WriteFile(generated, append(append([]byte(nil), want...), []byte("foreign\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	change, err = implementation.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed || !strings.Contains(change.Warning, "discarded") {
		t.Fatalf("repair Wire = %#v, %v", change, err)
	}
	if err = os.WriteFile(generated, append(append([]byte(nil), want...), []byte("foreign\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if change, err = implementation.Unwire(context.Background(), root, dep, exp); err != nil || !change.Changed {
		t.Fatalf("Unwire = %#v, %v", change, err)
	}
	if got := mustRead(t, filepath.Join(root, "Acme.App.csproj")); !bytes.Equal(got, original) {
		t.Fatalf("project not restored = %q", got)
	}
	if _, statErr := os.Stat(generated); !os.IsNotExist(statErr) {
		t.Fatalf("generated remains: %v", statErr)
	}
}

func TestDetectFSharpAndRequireVendoring(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "Acme.App.fsproj"), []byte("<Project />\n"))
	if ok, variant, err := (Adapter{}).Detect(root); err != nil || !ok || variant != "msbuild-fsharp" {
		t.Fatalf("Detect = %v, %q, %v", ok, variant, err)
	}
	_, err := (Adapter{}).Wire(context.Background(), root, adapter.Dependency{ID: "acme-lib"}, adapter.Export{Ecosystem: "nuget", Name: "Acme.LibUtils"}, adapter.Locked{})
	if !adapter.IsNotWirable(err) {
		t.Fatalf("missing vendor error = %v", err)
	}
}

func mustWrite(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
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
