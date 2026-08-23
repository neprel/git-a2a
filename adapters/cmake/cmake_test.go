package cmake

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

func TestGoldenLifecycle(t *testing.T) {
	root := t.TempDir()
	original := []byte("cmake_minimum_required(VERSION 3.20)\nproject(consumer)\n")
	if err := os.WriteFile(filepath.Join(root, rootFile), original, 0o644); err != nil {
		t.Fatal(err)
	}
	implementation := Adapter{}
	dep := adapter.Dependency{ID: "acme-native", Vendor: &manifest.Vendor{Mode: "submodule"}}
	exp := adapter.Export{Ecosystem: "cmake", Name: "acme::native", Path: "cpp"}
	locked := adapter.Locked{Path: "modules/native", Commit: strings.Repeat("a", 40), Vendor: &manifest.LockedVendor{Mode: "submodule", Path: "deps/acme-native"}}
	change, err := implementation.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed {
		t.Fatalf("Wire = %#v, %v", change, err)
	}
	want := header + "# git-a2a:begin acme-native\nadd_subdirectory(\"deps/acme-native/modules/native/cpp\")\n# git-a2a:end acme-native\n"
	if got, readErr := os.ReadFile(filepath.Join(root, generatedFile)); readErr != nil || string(got) != want {
		t.Fatalf("generated = %q, %v", got, readErr)
	}
	if change, err = implementation.Wire(context.Background(), root, dep, exp, locked); err != nil || change.Changed {
		t.Fatalf("second Wire = %#v, %v", change, err)
	}
	if findings, driftErr := implementation.Drift(context.Background(), root, dep, exp, locked); driftErr != nil || len(findings) != 0 {
		t.Fatalf("Drift = %#v, %v", findings, driftErr)
	}
	if err = os.WriteFile(filepath.Join(root, generatedFile), []byte(strings.Replace(want, "cpp", "wrong", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if findings, driftErr := implementation.Drift(context.Background(), root, dep, exp, locked); driftErr != nil || len(findings) != 1 {
		t.Fatalf("changed Drift = %#v, %v", findings, driftErr)
	}
	if _, err = implementation.Wire(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatal(err)
	}
	if change, err = implementation.Unwire(context.Background(), root, dep, exp); err != nil || !change.Changed {
		t.Fatalf("Unwire = %#v, %v", change, err)
	}
	if got, readErr := os.ReadFile(filepath.Join(root, rootFile)); readErr != nil || !bytes.Equal(got, original) {
		t.Fatalf("root not restored = %q, %v", got, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, generatedFile)); !os.IsNotExist(statErr) {
		t.Fatalf("generated file remains: %v", statErr)
	}
}

func TestRequiresVendoringAndSortsBlocks(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, rootFile), []byte("project(consumer)\n"), 0o644)
	implementation := Adapter{}
	exp := adapter.Export{Ecosystem: "cmake", Name: "acme::native"}
	if _, err := implementation.Wire(context.Background(), root, adapter.Dependency{ID: "acme-z"}, exp, adapter.Locked{}); !adapter.IsNotWirable(err) {
		t.Fatalf("error = %v", err)
	}
	for _, id := range []string{"acme-z", "acme-a"} {
		dep := adapter.Dependency{ID: id, Vendor: &manifest.Vendor{Mode: "copy"}}
		locked := adapter.Locked{Vendor: &manifest.LockedVendor{Mode: "copy", Path: "deps/" + id}}
		if _, err := implementation.Wire(context.Background(), root, dep, exp, locked); err != nil {
			t.Fatal(err)
		}
	}
	body, _ := os.ReadFile(filepath.Join(root, generatedFile))
	if strings.Index(string(body), "acme-a") > strings.Index(string(body), "acme-z") {
		t.Fatalf("blocks not sorted:\n%s", body)
	}
}

func TestOwnedGeneratedFileRepairsForeignContentAndUnwireNeverFails(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, rootFile), []byte("project(consumer)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	implementation := Adapter{}
	dep := adapter.Dependency{ID: "acme-native", Vendor: &manifest.Vendor{Mode: "copy"}}
	exp := adapter.Export{Ecosystem: "cmake", Name: "acme::native"}
	locked := adapter.Locked{Vendor: &manifest.LockedVendor{Mode: "copy", Path: "deps/acme-native"}}
	if _, err := implementation.Wire(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, generatedFile)
	want, _ := os.ReadFile(path)
	if err := os.WriteFile(path, append(append([]byte(nil), want...), []byte("human_line()\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	change, err := implementation.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed || !strings.Contains(change.Warning, "discarded") {
		t.Fatalf("repair Wire = %#v, %v", change, err)
	}
	if got, readErr := os.ReadFile(path); readErr != nil || !bytes.Equal(got, want) {
		t.Fatalf("repaired generated file = %q, %v", got, readErr)
	}
	if err := os.WriteFile(path, append(append([]byte(nil), want...), []byte("human_line()\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	change, err = implementation.Unwire(context.Background(), root, dep, exp)
	if err != nil || !change.Changed {
		t.Fatalf("Unwire = %#v, %v", change, err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("generated file remains: %v", statErr)
	}
}
