package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"status", dep.ID, "--offline"}); code != 1 {
		t.Fatalf("exit %d out=%s err=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "npm clean, pypi unwired, golang unwired") {
		t.Fatalf("wiring state missing:\n%s", out.String())
	}
}
