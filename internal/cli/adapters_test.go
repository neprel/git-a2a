package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/manifest"
)

func TestWireAllPolyglotUsesOneCommit(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join("..", "..", "testdata", "consumer-polyglot")
	for _, name := range []string{"package.json", "pyproject.toml", "go.mod"} {
		b, err := os.ReadFile(filepath.Join(fixture, name))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(root, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dep := manifest.Dependency{ID: "acme-lib-utils", Git: "https://github.com/acme/lib-utils.git", Ref: "main", Track: "locked"}
	module := &manifest.Manifest{Module: manifest.Module{Exports: []manifest.Export{{Ecosystem: "npm", Name: "@acme/lib-utils"}, {Ecosystem: "pypi", Name: "acme-lib-utils"}, {Ecosystem: "golang", Name: "acme.dev/lib-utils"}}}}
	commit := strings.Repeat("a", 40)
	locked := manifest.LockedDependency{Commit: commit}
	if err := wireAll(context.Background(), root, dep, module, locked, false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"package.json", "pyproject.toml", "go.mod"} {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		want := commit
		if name == "go.mod" {
			want = commit[:12]
		}
		if !strings.Contains(string(b), want) {
			t.Errorf("%s does not pin %s:\n%s", name, want, b)
		}
	}
}
