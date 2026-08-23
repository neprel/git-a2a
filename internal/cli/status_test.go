package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/adapters/composer"
	"github.com/neprel/git-a2a/internal/cache"
	lockfile "github.com/neprel/git-a2a/internal/lock"
	"github.com/neprel/git-a2a/internal/manifest"
)

func TestStatusReportsEachPolyglotWiringStateWithoutPanic(t *testing.T) {
	root := t.TempDir()
	commit := strings.Repeat("a", 40)
	gitURL := "https://github.com/acme/lib.git"
	dep := manifest.Dependency{ID: "acme-lib", Git: gitURL, Ref: "main", Path: ".", Track: "locked"}
	own := &manifest.Manifest{Schema: 1, Module: manifest.Module{ID: "acme-app"}, Dependencies: []manifest.Dependency{dep}}
	depManifest := &manifest.Manifest{Schema: 1, Module: manifest.Module{ID: "acme-lib", Exports: []manifest.Export{
		{Ecosystem: "npm", Name: "@acme/lib"},
		{Ecosystem: "pypi", Name: "acme-lib"},
		{Ecosystem: "golang", Name: "acme.dev/lib"},
		{Ecosystem: "composer", Name: "acme/lib"},
	}}}
	ownRaw, _ := manifest.Marshal(own)
	depRaw, _ := manifest.Marshal(depManifest)
	if err := os.WriteFile(filepath.Join(root, "a2amodule.yml"), ownRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cache.Save(root, dep.ID, depRaw, commit, "test"); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(depRaw)
	if err := lockfile.Write(root, &manifest.Lock{Schema: 1, Dependencies: map[string]manifest.LockedDependency{
		dep.ID: {Git: gitURL, Ref: "main", Path: ".", Commit: commit, Manifest: "sha256:" + hex.EncodeToString(sum[:])},
	}}); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"package.json":   "{\n  \"dependencies\": {\n    \"@acme/lib\": \"git+https://github.com/acme/lib.git#" + commit + "\"\n  }\n}\n",
		"pyproject.toml": "[project]\nname = \"consumer\"\ndependencies = []\n",
		"go.mod":         "module acme.dev/consumer\n\ngo 1.24\n",
		"composer.json":  "{\"name\":\"acme/consumer\",\"require\":{}}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := (composer.Adapter{}).Wire(context.Background(), root, dep, depManifest.Module.Exports[3], manifest.LockedDependency{Git: gitURL, Commit: commit}); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"status", dep.ID, "--offline", "-v"}); code != 1 {
		t.Fatalf("exit %d out=%s err=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "npm clean, pypi unwired, golang unwired, composer clean") {
		t.Fatalf("wiring state missing:\n%s", out.String())
	}
	lines := strings.Split(out.String(), "\n")
	if len(lines) < 2 || strings.Index(lines[0], "SOURCE") != strings.Index(lines[1], "canonical") ||
		strings.Index(lines[0], "WIRING") != strings.Index(lines[1], "npm clean") ||
		strings.Index(lines[0], "SYNC") != strings.LastIndex(lines[1], "none") {
		t.Fatalf("status columns are not aligned:\n%s", out.String())
	}
	if strings.Contains(out.String(), "form-verified") {
		t.Fatalf("verified adapter still reports pending integration:\n%s", out.String())
	}
	if strings.Contains(strings.SplitN(out.String(), "\n", 2)[0], "VENDOR") {
		t.Fatalf("non-vendored status unexpectedly has VENDOR column:\n%s", out.String())
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"status", dep.ID, "--offline", "--json"}); code != 1 {
		t.Fatalf("json exit %d out=%s err=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), `"vendor": "none"`) {
		t.Fatalf("JSON must always include vendor state:\n%s", out.String())
	}
}

func TestStatusTreatsMissingManagedBlockAsHealthyNoneAndUsesModuleFooter(t *testing.T) {
	root := t.TempDir()
	own := &manifest.Manifest{Schema: 1, Module: manifest.Module{ID: "consumer-app"}}
	raw, err := manifest.Marshal(own)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "a2amodule.yml"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err = lockfile.Write(root, &manifest.Lock{Schema: 1, Dependencies: map[string]manifest.LockedDependency{}}); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Human instructions\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"status", "--offline"}); code != 0 {
		t.Fatalf("exit %d out=%s err=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "consumer-app: manifest valid · agents none · roster none") {
		t.Fatalf("module footer missing:\n%s", out.String())
	}
	if strings.Contains(out.String(), "consumer-app\tself") {
		t.Fatalf("own module leaked into dependency table:\n%s", out.String())
	}
	if got := strings.TrimSpace(errOut.String()); got != "0 dependencies: clean" {
		t.Fatalf("summary = %q", got)
	}
}

func TestStatusCallsOnlyExistingDifferentManagedBlockStale(t *testing.T) {
	root := t.TempDir()
	own := &manifest.Manifest{Schema: 1, Module: manifest.Module{ID: "consumer-app"}}
	raw, _ := manifest.Marshal(own)
	if err := os.WriteFile(filepath.Join(root, "a2amodule.yml"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := lockfile.Write(root, &manifest.Lock{Schema: 1, Dependencies: map[string]manifest.LockedDependency{}}); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"sync"}); code != 0 {
		t.Fatalf("sync exit %d: %s", code, errOut.String())
	}
	path := filepath.Join(root, "AGENTS.md")
	b, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(b), "consumer-app", "wrong-app", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"status", "--offline"}); code != 1 {
		t.Fatalf("exit %d out=%s err=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "roster stale") {
		t.Fatalf("stale footer missing:\n%s", out.String())
	}
}
