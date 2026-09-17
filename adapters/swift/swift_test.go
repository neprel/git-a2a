package swift

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
	fixture := filepath.Join("..", "..", "testdata", "consumer-swift")
	original, _ := os.ReadFile(filepath.Join(fixture, "Package.swift"))
	_ = os.WriteFile(filepath.Join(root, "Package.swift"), original, 0o644)
	dep := adapter.Dependency{Name: "acme-lib", Git: "https://github.com/acme/lib-utils.git", Ref: "main"}
	exp := adapter.Export{Adapter: "swift", Name: "AcmeLibUtils"}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	a := Adapter{}
	change, err := a.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed {
		t.Fatalf("wire=%#v err=%v", change, err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "Package.swift"))
	want, _ := os.ReadFile(filepath.Join(fixture, "Package.golden.swift"))
	if string(got) != string(want) {
		t.Fatalf("golden differs\ngot:\n%s\nwant:\n%s", got, want)
	}
	if change, err = a.Wire(context.Background(), root, dep, exp, locked); err != nil || change.Changed {
		t.Fatalf("second wire=%#v err=%v", change, err)
	}
	if findings, err := a.Drift(context.Background(), root, dep, exp, locked); err != nil || len(findings) != 0 {
		t.Fatalf("drift=%v err=%v", findings, err)
	}
	manifestPath := filepath.Join(root, "Package.swift")
	branchPin := strings.Replace(string(mustRead(t, manifestPath)), `revision: "`+locked.Commit+`"`, `branch: "main"`, 1)
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
	got, _ = os.ReadFile(filepath.Join(root, "Package.swift"))
	if string(got) != string(original) {
		t.Fatalf("unwire differs\ngot:\n%s\nwant:\n%s", got, original)
	}
}

func TestWireAndUnwireSingleLineNonEmptyDependencies(t *testing.T) {
	root := t.TempDir()
	original := `// swift-tools-version: 6.0
import PackageDescription
let package = Package(name: "Consumer", dependencies: [.package(path: "../unrelated")], targets: [])
`
	if err := os.WriteFile(filepath.Join(root, "Package.swift"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{Git: "https://example.test/native-lib.git"}
	exp := adapter.Export{Name: "NativeLib"}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	if _, err := (Adapter{}).Wire(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatal(err)
	}
	if _, err := (Adapter{}).Unwire(context.Background(), root, dep, exp); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, filepath.Join(root, "Package.swift"))); got != original {
		t.Fatalf("unwire did not restore inline manifest\ngot:\n%s\nwant:\n%s", got, original)
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
