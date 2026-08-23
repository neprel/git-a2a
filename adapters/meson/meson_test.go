package meson

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

func fixture() (adapter.Dependency, adapter.Export, adapter.Locked) {
	dep := adapter.Dependency{ID: "acme-lib-utils", Vendor: &manifest.Vendor{Mode: "copy", Path: "subprojects/acme-lib-utils"}}
	exp := adapter.Export{Ecosystem: "meson", Name: "acme-lib-utils"}
	locked := adapter.Locked{Vendor: &manifest.LockedVendor{Mode: "copy", Path: "subprojects/acme-lib-utils"}}
	return dep, exp, locked
}

func TestGoldenLifecycle(t *testing.T) {
	root := t.TempDir()
	original := []byte("project('consumer', 'cpp')\n")
	if err := os.WriteFile(filepath.Join(root, rootFile), original, 0o644); err != nil {
		t.Fatal(err)
	}
	dep, exp, locked := fixture()
	implementation := Adapter{}
	change, err := implementation.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed {
		t.Fatalf("Wire = %#v, %v", change, err)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "testdata", "consumer-meson", "generated.golden.meson"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, generatedFile))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("generated = %q, %v", got, err)
	}
	if change, err = implementation.Wire(context.Background(), root, dep, exp, locked); err != nil || change.Changed {
		t.Fatalf("second Wire = %#v, %v", change, err)
	}
	if findings, driftErr := implementation.Drift(context.Background(), root, dep, exp, locked); driftErr != nil || len(findings) != 0 {
		t.Fatalf("Drift = %#v, %v", findings, driftErr)
	}
	if err = os.WriteFile(filepath.Join(root, generatedFile), append(got, []byte("foreign()\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	change, err = implementation.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed || !strings.Contains(change.Warning, "discarded") {
		t.Fatalf("repair = %#v, %v", change, err)
	}
	if repaired, readErr := os.ReadFile(filepath.Join(root, generatedFile)); readErr != nil || !bytes.Equal(repaired, want) {
		t.Fatalf("repaired = %q, %v", repaired, readErr)
	}
	if err = os.WriteFile(filepath.Join(root, generatedFile), append(want, []byte("foreign()\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	change, err = implementation.Unwire(context.Background(), root, dep, exp)
	if err != nil || !change.Changed {
		t.Fatalf("Unwire = %#v, %v", change, err)
	}
	if body, readErr := os.ReadFile(filepath.Join(root, rootFile)); readErr != nil || !bytes.Equal(body, original) {
		t.Fatalf("root = %q, %v", body, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, generatedFile)); !os.IsNotExist(statErr) {
		t.Fatalf("generated remains: %v", statErr)
	}
}

func TestRequiresVendoringAndSubprojectsPath(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, rootFile), []byte("project('consumer')\n"), 0o644)
	implementation := Adapter{}
	exp := adapter.Export{Ecosystem: "meson", Name: "acme-lib-utils"}
	if _, err := implementation.Wire(context.Background(), root, adapter.Dependency{ID: "acme-lib-utils"}, exp, adapter.Locked{}); !adapter.IsNotWirable(err) {
		t.Fatalf("missing vendor error = %v", err)
	}
	dep, exp, locked := fixture()
	locked.Vendor.Path = "deps/acme-lib-utils"
	if _, err := implementation.Wire(context.Background(), root, dep, exp, locked); err == nil || !strings.Contains(err.Error(), "--vendor-path subprojects/acme-lib-utils") {
		t.Fatalf("path error = %v", err)
	}
}

func TestSortsBlocks(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, rootFile), []byte("project('consumer')\n"), 0o644)
	for _, id := range []string{"acme-z", "acme-a"} {
		dep := adapter.Dependency{ID: id, Vendor: &manifest.Vendor{Mode: "copy", Path: "subprojects/" + id}}
		locked := adapter.Locked{Vendor: &manifest.LockedVendor{Mode: "copy", Path: "subprojects/" + id}}
		if _, err := (Adapter{}).Wire(context.Background(), root, dep, adapter.Export{Ecosystem: "meson", Name: id}, locked); err != nil {
			t.Fatal(err)
		}
	}
	body, _ := os.ReadFile(filepath.Join(root, generatedFile))
	if strings.Index(string(body), "acme-a") > strings.Index(string(body), "acme-z") {
		t.Fatalf("not sorted:\n%s", body)
	}
}
