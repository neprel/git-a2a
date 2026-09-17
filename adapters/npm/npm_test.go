package npm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/adapter"
)

func TestWireGoldenIdempotentUnwire(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join("..", "..", "testdata", "consumer-npm")
	original := copyFile(t, filepath.Join(fixture, "package.json"), filepath.Join(root, "package.json"))
	dep := adapter.Dependency{Name: "lib-utils", Git: "https://github.com/acme/lib-utils.git", Ref: "main"}
	exp := adapter.Export{Adapter: "npm", Name: "@acme/lib-utils"}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	a := Adapter{}
	change, err := a.Wire(context.Background(), root, dep, exp, locked)
	if err != nil {
		t.Fatal(err)
	}
	if !change.Changed {
		t.Fatal("first wire did not change")
	}
	got, _ := os.ReadFile(filepath.Join(root, "package.json"))
	golden, _ := os.ReadFile(filepath.Join(fixture, "package.golden.json"))
	assertJSONEqual(t, got, golden)
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
	var gotDoc, wantDoc any
	_ = json.Unmarshal(mustRead(t, filepath.Join(root, "package.json")), &gotDoc)
	_ = json.Unmarshal(original, &wantDoc)
	if !deepEqual(gotDoc, wantDoc) {
		t.Fatal("unwire did not restore dependency data")
	}
}

func TestDetectVariants(t *testing.T) {
	for _, tc := range []struct{ name, want string }{{"consumer-yarn", "yarn-berry"}, {"consumer-pnpm", "pnpm"}, {"consumer-npm", "npm"}} {
		ok, got, err := (Adapter{}).Detect(filepath.Join("..", "..", "testdata", tc.name))
		if err != nil || !ok || string(got) != tc.want {
			t.Errorf("%s: %v %q %v", tc.name, ok, got, err)
		}
	}
}

func TestDetectPackageManagerVariantsWithoutLockfiles(t *testing.T) {
	for _, tc := range []struct{ packageManager, want string }{
		{"npm@11.0.0", "npm"},
		{"yarn@4.6.0", "yarn-berry"},
		{"pnpm@10.0.0", "pnpm"},
		{"bun@1.2.0", "bun"},
	} {
		root := t.TempDir()
		content := fmt.Sprintf("{\"name\":\"consumer\",\"packageManager\":%q}\n", tc.packageManager)
		if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		ok, got, err := (Adapter{}).Detect(root)
		if err != nil || !ok || string(got) != tc.want {
			t.Errorf("%s: ok=%v variant=%q err=%v", tc.packageManager, ok, got, err)
		}
	}
}

func TestWireUsesLockedSourceAndExactCommit(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{\"name\":\"consumer\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("c", 40)
	dep := adapter.Dependency{Git: "https://stale.example.test/lib.git", Ref: "main"}
	exp := adapter.Export{Adapter: "npm", Name: "@acme/lib"}
	locked := adapter.Locked{Git: "https://canonical.example.test/lib.git", Commit: commit}
	if _, err := (Adapter{}).Wire(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatal(err)
	}
	got := string(mustRead(t, filepath.Join(root, "package.json")))
	if !strings.Contains(got, "git+"+locked.Git+"#"+commit) || strings.Contains(got, dep.Git) || strings.Contains(got, "#main") {
		t.Fatalf("dependency was not pinned to the locked source and commit:\n%s", got)
	}
}
func TestDriftMissingEntryIsUnwired(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{\"dependencies\":{}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err := (Adapter{}).Drift(context.Background(), root,
		adapter.Dependency{Git: "https://github.com/acme/lib.git"},
		adapter.Export{Adapter: "npm", Name: "@acme/lib"},
		adapter.Locked{Git: "https://github.com/acme/lib.git", Commit: strings.Repeat("a", 40)})
	if err != nil || len(findings) != 1 {
		t.Fatalf("findings=%v err=%v", findings, err)
	}
}

func TestDriftDetectsTamperedManifestPinWithoutPackageLock(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{\"dependencies\":{\"@acme/lib\":\"git+https://github.com/acme/lib.git#deadbeef\"}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err := (Adapter{}).Drift(context.Background(), root,
		adapter.Dependency{Git: "https://github.com/acme/lib.git"},
		adapter.Export{Adapter: "npm", Name: "@acme/lib"},
		adapter.Locked{Git: "https://github.com/acme/lib.git", Commit: strings.Repeat("a", 40)})
	if err != nil || len(findings) != 1 || findings[0].Got == "" {
		t.Fatalf("findings=%v err=%v", findings, err)
	}
}

func TestRemoveInlineDependencyRestoresSurroundingBytes(t *testing.T) {
	original := []byte("{\"name\":\"consumer\",\"dependencies\":{\"left-pad\":\"^1.0.0\",\"@acme/lib\":\"git+https://example.test/lib.git#abc\"},\"private\":true}\n")
	want := []byte("{\"name\":\"consumer\",\"dependencies\":{\"left-pad\":\"^1.0.0\"},\"private\":true}\n")
	got, changed, err := removeDependency(original, "@acme/lib")
	if err != nil || !changed || !bytes.Equal(got, want) {
		t.Fatalf("changed=%v err=%v\ngot  %s\nwant %s", changed, err, got, want)
	}
}

func TestUpdateInlineDependencyPreservesSurroundingBytes(t *testing.T) {
	original := []byte("{\"dependencies\":{\"@acme/lib\":\"old\",\"left-pad\":\"^1.0.0\"}}\n")
	want := []byte("{\"dependencies\":{\"@acme/lib\":\"new\",\"left-pad\":\"^1.0.0\"}}\n")
	got, changed, err := setDependency(original, "@acme/lib", "new")
	if err != nil || !changed || !bytes.Equal(got, want) {
		t.Fatalf("changed=%v err=%v\ngot  %s\nwant %s", changed, err, got, want)
	}
}
func TestNPMPullInstallsWithoutScripts(t *testing.T) {
	got := strings.Join(refreshCommand("npm", "@acme/lib"), " ")
	want := "npm install --ignore-scripts --no-audit --no-fund"
	if got != want {
		t.Fatalf("refresh command = %q, want %q", got, want)
	}
}

func TestYarnPullInstallsWithoutBuildScripts(t *testing.T) {
	got := strings.Join(refreshCommand("yarn-berry", "@acme/lib"), " ")
	if got != "yarn install --mode=skip-build" {
		t.Fatalf("refresh command = %q", got)
	}
}

func TestYarnBerryDependencyURLUsesGitTransport(t *testing.T) {
	commit := strings.Repeat("a", 40)
	for _, gitURL := range []string{
		"git@example.test:acme/lib.git",
		"ssh://git@example.test/acme/lib.git",
		"https://example.test/acme/lib.git",
	} {
		got := dependencyURL(
			adapter.Locked{Git: gitURL, Commit: commit},
			"yarn-berry",
			".",
		)
		if !strings.HasPrefix(got, "git+") || !strings.HasSuffix(got, "#commit="+commit) {
			t.Errorf("dependencyURL(%q) = %q", gitURL, got)
		}
	}
}

func TestYarnBerryKeepsNativeGitProtocol(t *testing.T) {
	commit := strings.Repeat("a", 40)
	locked := adapter.Locked{Git: "git://127.0.0.1/library.git", Commit: commit}
	got := dependencyURL(locked, "yarn-berry", "")
	want := locked.Git + "#commit=" + commit
	if got != want {
		t.Fatalf("dependency URL = %q, want %q", got, want)
	}
}

func TestInspectRequiresExactScopedNativeLockEntry(t *testing.T) {
	root := t.TempDir()
	commit := strings.Repeat("a", 40)
	dep := adapter.Dependency{Git: "https://example.test/lib.git"}
	exp := adapter.Export{Name: "@acme/lib"}
	locked := adapter.Locked{Git: dep.Git, Commit: commit}
	if _, err := (Adapter{}).Wire(context.Background(), rootWithPackageJSON(t, root), dep, exp, locked); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@acme", "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	lock := `{"lockfileVersion":3,"packages":{"node_modules/other":{"resolved":"git+https://example.test/other.git#` + commit + `"},"node_modules/@acme/lib":{"resolved":"git+https://example.test/lib.git#` + strings.Repeat("b", 40) + `"}}}`
	if err := os.WriteFile(filepath.Join(root, "package-lock.json"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err := (Adapter{}).Inspect(context.Background(), root, dep, exp, locked)
	if err != nil || len(findings) != 1 || findings[0].File != "package-lock.json" {
		t.Fatalf("findings=%v err=%v", findings, err)
	}
}

func rootWithPackageJSON(t *testing.T, root string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{\"name\":\"consumer\",\"dependencies\":{}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
func copyFile(t *testing.T, src, dst string) []byte {
	t.Helper()
	b := mustRead(t, src)
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return b
}
func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func assertJSONEqual(t *testing.T, a, b []byte) {
	t.Helper()
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil || !deepEqual(x, y) {
		t.Fatalf("json differs\ngot %s\nwant %s", a, b)
	}
}
func deepEqual(a, b any) bool { return fmtJSON(a) == fmtJSON(b) }
func fmtJSON(v any) string    { b, _ := json.Marshal(v); return string(b) }
