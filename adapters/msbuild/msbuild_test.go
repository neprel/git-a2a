package msbuild

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/adapter"
)

func TestGoldenRepairAndUnwireLifecycle(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join("..", "..", "testdata", "consumer-msbuild")
	original := mustRead(t, filepath.Join(fixture, "Acme.App.csproj"))
	mustWrite(t, filepath.Join(root, "Acme.App.csproj"), original)
	mustWrite(t, filepath.Join(root, "deps", "acme-lib", "dotnet", "Acme.LibUtils.csproj"), []byte("<Project />\n"))
	implementation := Adapter{}
	dep := adapter.Dependency{Name: "acme-lib"}
	exp := adapter.Export{Adapter: "nuget", Name: "Acme.LibUtils", Path: "deps/acme-lib/dotnet"}
	locked := adapter.Locked{Commit: strings.Repeat("a", 40)}
	change, err := implementation.wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed {
		t.Fatalf("Wire = %#v, %v", change, err)
	}
	want := mustRead(t, filepath.Join(fixture, "generated.golden.targets"))
	generated := filepath.Join(root, filepath.FromSlash(generatedFile))
	if got := mustRead(t, generated); !bytes.Equal(got, want) {
		t.Fatalf("golden differs\ngot:\n%s\nwant:\n%s", got, want)
	}
	if change, err = implementation.wire(context.Background(), root, dep, exp, locked); err != nil || change.Changed {
		t.Fatalf("second Wire = %#v, %v", change, err)
	}
	if findings, driftErr := implementation.inspectDeclaration(context.Background(), root, dep, exp, locked); driftErr != nil || len(findings) != 0 {
		t.Fatalf("Drift = %#v, %v", findings, driftErr)
	}
	if err = os.WriteFile(generated, append(append([]byte(nil), want...), []byte("foreign\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	change, err = implementation.wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed || !strings.Contains(change.Warning, "discarded") {
		t.Fatalf("repair Wire = %#v, %v", change, err)
	}
	if err = os.WriteFile(generated, append(append([]byte(nil), want...), []byte("foreign\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if change, err = implementation.unwire(context.Background(), root, dep, exp); err != nil || !change.Changed {
		t.Fatalf("Unwire = %#v, %v", change, err)
	}
	if got := mustRead(t, filepath.Join(root, "Acme.App.csproj")); !bytes.Equal(got, original) {
		t.Fatalf("project not restored = %q", got)
	}
	if _, statErr := os.Stat(generated); !os.IsNotExist(statErr) {
		t.Fatalf("generated remains: %v", statErr)
	}
}

func TestDetectFSharpAndRequireCheckoutPath(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "Acme.App.fsproj"), []byte("<Project />\n"))
	if ok, variant, err := (Adapter{}).Detect(root); err != nil || !ok || variant != "msbuild-fsharp" {
		t.Fatalf("Detect = %v, %q, %v", ok, variant, err)
	}
	original := mustRead(t, filepath.Join(root, "Acme.App.fsproj"))
	if err := (Adapter{}).Capability(root, adapter.Dependency{Name: "acme-lib"}, adapter.Export{Adapter: "nuget", Name: "Acme.LibUtils"}); !adapter.IsNotWirable(err) {
		t.Fatalf("Capability error = %v", err)
	}
	if got := mustRead(t, filepath.Join(root, "Acme.App.fsproj")); !bytes.Equal(got, original) {
		t.Fatal("Capability mutated the consumer")
	}
	_, err := (Adapter{}).wire(context.Background(), root, adapter.Dependency{Name: "acme-lib"}, adapter.Export{Adapter: "nuget", Name: "Acme.LibUtils"}, adapter.Locked{})
	if !adapter.IsNotWirable(err) {
		t.Fatalf("missing checkout path error = %v", err)
	}
}

func TestInspectReportsMissingCheckoutProject(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "Acme.App.csproj"), []byte("<Project />\n"))
	dep := adapter.Dependency{Name: "acme-lib"}
	exp := adapter.Export{Adapter: "nuget", Name: "Acme.LibUtils", Path: "deps/acme-lib/Acme.LibUtils.csproj"}
	findings, err := (Adapter{}).Inspect(context.Background(), root, dep, exp, adapter.Locked{})
	if err != nil || len(findings) != 3 || findings[0].Got != "" || findings[1].Got == "" || findings[2].Got != "" {
		t.Fatalf("Inspect = %#v, %v", findings, err)
	}
}

func TestNativePullAndRemoveResolveProjectReference(t *testing.T) {
	if _, err := exec.LookPath("dotnet"); err != nil {
		t.Skip("dotnet is not installed")
	}
	root := t.TempDir()
	consumer := []byte(`<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup>
</Project>
`)
	component := []byte(`<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup>
</Project>
`)
	mustWrite(t, filepath.Join(root, "Consumer.csproj"), consumer)
	mustWrite(t, filepath.Join(root, "deps", "acme", "Acme.Lib.csproj"), component)
	dep := adapter.Dependency{Name: "acme"}
	exp := adapter.Export{Adapter: "nuget", Name: "Acme.Lib", Path: "deps/acme/Acme.Lib.csproj"}
	a := Adapter{}
	change, err := a.Pull(context.Background(), root, dep, exp, adapter.Locked{Commit: strings.Repeat("a", 40)})
	if err != nil || !change.Changed {
		t.Fatalf("Pull = %#v, %v", change, err)
	}
	assets, err := os.ReadFile(filepath.Join(root, "obj", "project.assets.json"))
	if err != nil || !bytes.Contains(assets, []byte("Acme.Lib")) {
		t.Fatalf("dotnet restore did not resolve the project reference: contains=%v, err=%v", bytes.Contains(assets, []byte("Acme.Lib")), err)
	}
	if findings, inspectErr := a.Inspect(context.Background(), root, dep, exp, adapter.Locked{}); inspectErr != nil || len(findings) != 0 {
		t.Fatalf("Inspect = %#v, %v", findings, inspectErr)
	}
	if change, err = a.Remove(context.Background(), root, dep, exp, adapter.Locked{}); err != nil || !change.Changed {
		t.Fatalf("Remove = %#v, %v", change, err)
	}
	if _, err = os.Stat(filepath.Join(root, filepath.FromSlash(generatedFile))); !os.IsNotExist(err) {
		t.Fatalf("generated integration remains after Remove: %v", err)
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
