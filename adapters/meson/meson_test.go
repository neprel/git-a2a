package meson

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/v2/internal/adapter"
)

func fixture() (adapter.Dependency, adapter.Export, adapter.Locked) {
	return adapter.Dependency{Name: "acme-lib-utils"},
		adapter.Export{Adapter: "meson", Name: "acme-lib-utils", Path: "deps/acme-lib-utils"},
		adapter.Locked{Commit: strings.Repeat("a", 40)}
}

func TestGoldenLifecycle(t *testing.T) {
	root := t.TempDir()
	original := []byte("project('consumer', 'cpp')\n")
	if err := os.WriteFile(filepath.Join(root, rootFile), original, 0o644); err != nil {
		t.Fatal(err)
	}
	dep, exp, locked := fixture()
	implementation := Adapter{}
	change, err := implementation.wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed {
		t.Fatalf("Wire = %#v, %v", change, err)
	}
	want := append(append([]byte(nil), original...), []byte(regionBegin+"subdir('deps/acme-lib-utils') # git-a2a: acme-lib-utils\n"+regionEnd)...)
	path := filepath.Join(root, rootFile)
	if got, readErr := os.ReadFile(path); readErr != nil || !bytes.Equal(got, want) {
		t.Fatalf("root = %q, %v", got, readErr)
	}
	if change, err = implementation.wire(context.Background(), root, dep, exp, locked); err != nil || change.Changed {
		t.Fatalf("second Wire = %#v, %v", change, err)
	}
	if findings, driftErr := implementation.inspectDeclaration(context.Background(), root, dep, exp, locked); driftErr != nil || len(findings) != 0 {
		t.Fatalf("Drift = %#v, %v", findings, driftErr)
	}
	corrupt := bytes.Replace(want, []byte(regionEnd), []byte("foreign()\n"+regionEnd), 1)
	if err = os.WriteFile(path, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}
	if findings, driftErr := implementation.inspectDeclaration(context.Background(), root, dep, exp, locked); driftErr != nil || len(findings) != 1 {
		t.Fatalf("foreign Drift = %#v, %v", findings, driftErr)
	}
	change, err = implementation.wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed || !strings.Contains(change.Warning, "discarded") {
		t.Fatalf("repair = %#v, %v", change, err)
	}
	if change, err = implementation.unwire(context.Background(), root, dep, exp); err != nil || !change.Changed {
		t.Fatalf("Unwire = %#v, %v", change, err)
	}
	if body, readErr := os.ReadFile(path); readErr != nil || !bytes.Equal(body, original) {
		t.Fatalf("root = %q, %v", body, readErr)
	}
}

func TestRequiresCheckoutPathAndSortsOwnedLines(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, rootFile), []byte("project('consumer')\n"), 0o644)
	implementation := Adapter{}
	original, _ := os.ReadFile(filepath.Join(root, rootFile))
	if err := implementation.Capability(root, adapter.Dependency{Name: "acme"}, adapter.Export{Adapter: "meson", Name: "acme"}); !adapter.IsNotWirable(err) {
		t.Fatalf("Capability error = %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, rootFile)); !bytes.Equal(got, original) {
		t.Fatal("Capability mutated the consumer")
	}
	if _, err := implementation.wire(context.Background(), root, adapter.Dependency{Name: "acme"}, adapter.Export{Adapter: "meson", Name: "acme"}, adapter.Locked{}); !adapter.IsNotWirable(err) {
		t.Fatalf("missing checkout path error = %v", err)
	}
	for _, name := range []string{"acme-z", "acme-a"} {
		if _, err := implementation.wire(context.Background(), root, adapter.Dependency{Name: name}, adapter.Export{Adapter: "meson", Name: name, Path: "deps/" + name}, adapter.Locked{Commit: strings.Repeat("b", 40)}); err != nil {
			t.Fatal(err)
		}
	}
	body, _ := os.ReadFile(filepath.Join(root, rootFile))
	if strings.Index(string(body), "acme-a") > strings.Index(string(body), "acme-z") {
		t.Fatalf("owned lines not sorted:\n%s", body)
	}
	if change, err := implementation.unwire(context.Background(), root, adapter.Dependency{Name: "acme-a"}, adapter.Export{}); err != nil || !change.Changed {
		t.Fatalf("Unwire one = %#v, %v", change, err)
	}
	body, _ = os.ReadFile(filepath.Join(root, rootFile))
	if strings.Contains(string(body), "acme-a") || !strings.Contains(string(body), "acme-z") {
		t.Fatalf("Unwire removed unrelated entry:\n%s", body)
	}
}

func TestManagedRegionPrecedesConsumerUse(t *testing.T) {
	root := t.TempDir()
	original := []byte("project(\n  'consumer',\n  'cpp',\n)\nexecutable('consumer', 'main.cpp', dependencies: acme_dep)\n")
	if err := os.WriteFile(filepath.Join(root, rootFile), original, 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{Name: "acme"}
	exp := adapter.Export{Adapter: "meson", Name: "acme", Path: "deps/acme"}
	if _, err := (Adapter{}).wire(context.Background(), root, dep, exp, adapter.Locked{}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, rootFile))
	if err != nil {
		t.Fatal(err)
	}
	if regionAt, useAt := bytes.Index(body, []byte(regionBegin)), bytes.Index(body, []byte("executable(")); regionAt < 0 || regionAt > useAt {
		t.Fatalf("managed region does not precede consumer use:\n%s", body)
	}
	if _, err := (Adapter{}).unwire(context.Background(), root, dep, exp); err != nil {
		t.Fatal(err)
	}
	if restored, err := os.ReadFile(filepath.Join(root, rootFile)); err != nil || !bytes.Equal(restored, original) {
		t.Fatalf("root not byte-restored = %q, %v", restored, err)
	}
}

func TestInspectReportsMissingCheckout(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, rootFile), []byte("project('consumer')\n"), 0o644)
	dep, exp, locked := fixture()
	if _, err := (Adapter{}).wire(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatal(err)
	}
	findings, err := (Adapter{}).Inspect(context.Background(), root, dep, exp, locked)
	if err != nil || len(findings) != 1 || findings[0].Got != "missing" {
		t.Fatalf("Inspect = %#v, %v", findings, err)
	}
}

func TestMalformedOwnedRegionIsNotMutated(t *testing.T) {
	root := t.TempDir()
	original := []byte("project('consumer')\n" + regionBegin + "subdir('deps/acme') # git-a2a: acme\n")
	path := filepath.Join(root, rootFile)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	dep, exp, locked := fixture()
	if _, err := (Adapter{}).wire(context.Background(), root, dep, exp, locked); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("Wire error = %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, original) {
		t.Fatalf("malformed file mutated = %q, %v", got, err)
	}
}

func TestRemoveLastDependencyDeletesOwnedSetupTree(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, rootFile), []byte("project('consumer')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	build := filepath.Join(root, ".git-a2a", "build", "meson")
	if err := os.MkdirAll(build, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (Adapter{}).resolveAfterRemove(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(build); !os.IsNotExist(err) {
		t.Fatalf("owned setup tree remains: %v", err)
	}
}
