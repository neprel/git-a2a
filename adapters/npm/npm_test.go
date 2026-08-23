package npm

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/adapter"
	"github.com/neprel/git-a2a/internal/manifest"
)

func TestWireGoldenIdempotentUnwire(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join("..", "..", "testdata", "consumer-npm")
	original := copyFile(t, filepath.Join(fixture, "package.json"), filepath.Join(root, "package.json"))
	dep := adapter.Dependency{Git: "https://github.com/acme/lib-utils.git", Ref: "main", Track: "locked"}
	exp := adapter.Export{Ecosystem: "npm", Name: "@acme/lib-utils"}
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

func TestVendoredPathLifecycle(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{\"name\":\"consumer\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{ID: "acme-lib", Vendor: &manifest.Vendor{Mode: "copy"}}
	exp := adapter.Export{Ecosystem: "npm", Name: "@acme/lib", Path: "js"}
	locked := adapter.Locked{Vendor: &manifest.LockedVendor{Mode: "copy", Path: "deps/acme-lib"}}
	a := Adapter{}
	if change, err := a.Wire(context.Background(), root, dep, exp, locked); err != nil || !change.Changed {
		t.Fatalf("Wire=%#v %v", change, err)
	}
	if got := string(mustRead(t, filepath.Join(root, "package.json"))); !strings.Contains(got, `"@acme/lib": "file:deps/acme-lib/js"`) {
		t.Fatalf("path wiring:\n%s", got)
	}
	if findings, err := a.Drift(context.Background(), root, dep, exp, locked); err != nil || len(findings) != 0 {
		t.Fatalf("Drift=%v %v", findings, err)
	}
	if change, err := a.Unwire(context.Background(), root, dep, exp); err != nil || !change.Changed {
		t.Fatalf("Unwire=%#v %v", change, err)
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
func TestDriftMissingEntryIsUnwired(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{\"dependencies\":{}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err := (Adapter{}).Drift(context.Background(), root,
		adapter.Dependency{Git: "https://github.com/acme/lib.git"},
		adapter.Export{Ecosystem: "npm", Name: "@acme/lib"},
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
		adapter.Dependency{Git: "https://github.com/acme/lib.git", Track: "locked"},
		adapter.Export{Ecosystem: "npm", Name: "@acme/lib"},
		adapter.Locked{Git: "https://github.com/acme/lib.git", Commit: strings.Repeat("a", 40)})
	if err != nil || len(findings) != 1 || findings[0].Got == "" {
		t.Fatalf("findings=%v err=%v", findings, err)
	}
}

func TestRemoveInlineDependencyRestoresSurroundingBytes(t *testing.T) {
	original := []byte("{\"name\":\"consumer\",\"dependencies\":{\"left-pad\":\"^1.0.0\",\"@acme/lib\":\"git+file:///lib.git#abc\"},\"private\":true}\n")
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
func TestNPMRefreshOnlyUpdatesLockfileWithoutScripts(t *testing.T) {
	got := strings.Join(refreshCommand("npm", "@acme/lib"), " ")
	want := "npm install --package-lock-only --ignore-scripts --no-audit --no-fund"
	if got != want {
		t.Fatalf("refresh command = %q, want %q", got, want)
	}
}

func TestYarnRefreshUsesPinnedManifestRange(t *testing.T) {
	got := strings.Join(refreshCommand("yarn-berry", "@acme/lib"), " ")
	if got != "yarn install --mode=update-lockfile" {
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
			adapter.Dependency{Git: gitURL, Track: "locked"},
			adapter.Locked{Commit: commit},
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
	dep := adapter.Dependency{Git: "git://127.0.0.1/library.git", Track: "locked"}
	got := dependencyURL(dep, adapter.Locked{Commit: commit}, "yarn-berry", "")
	want := dep.Git + "#commit=" + commit
	if got != want {
		t.Fatalf("dependency URL = %q, want %q", got, want)
	}
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
