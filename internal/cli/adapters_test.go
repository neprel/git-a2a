package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/adapters/golang"
	"github.com/neprel/git-a2a/adapters/npm"
	"github.com/neprel/git-a2a/adapters/pypi"
	"github.com/neprel/git-a2a/internal/adapter"
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
	implementations := []adapter.Adapter{
		npm.Adapter{},
		pypi.Adapter{},
		golang.Adapter{ResolveVersion: func(_ context.Context, _, _, commit string) (string, error) {
			return "v0.0.0-20260822112233-" + commit[:12], nil
		}},
	}
	if _, err := wireAllUsing(context.Background(), root, dep, module, locked, false, implementations); err != nil {
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

func TestWireAllSkipsImplicitlyUnwirableAndRejectsExplicit(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"package.json":   "{\n  \"name\": \"consumer\",\n  \"dependencies\": {}\n}\n",
		"pyproject.toml": "[project]\nname = \"consumer\"\ndependencies = []\n",
		"go.mod":         "module acme.dev/consumer\n\ngo 1.24\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dep := manifest.Dependency{ID: "acme-lib", Git: "file:///tmp/acme-lib.git", Ref: "main", Track: "locked"}
	module := &manifest.Manifest{Module: manifest.Module{Exports: []manifest.Export{
		{Ecosystem: "npm", Name: "@acme/lib"},
		{Ecosystem: "pypi", Name: "acme-lib"},
		{Ecosystem: "golang", Name: "acme.dev/lib"},
	}}}
	locked := manifest.LockedDependency{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	outcomes, err := wireAll(context.Background(), root, dep, module, locked, false)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]wireOutcome{}
	for _, outcome := range outcomes {
		states[outcome.Ecosystem] = outcome
	}
	if !states["npm"].Wired || !states["pypi"].Wired || states["golang"].Wired || states["golang"].Reason == "" {
		t.Fatalf("outcomes: %#v", outcomes)
	}
	explicit := []string{"golang"}
	dep.Wire = &explicit
	if _, err = wireAll(context.Background(), root, dep, module, locked, false); err == nil {
		t.Fatal("explicitly requested unwirable ecosystem succeeded")
	}
}

type missingRefreshAdapter struct{}

func (missingRefreshAdapter) Ecosystem() string                            { return "demo" }
func (missingRefreshAdapter) Detect(string) (bool, adapter.Variant, error) { return true, "demo", nil }
func (missingRefreshAdapter) Wire(context.Context, string, adapter.Dependency, adapter.Export, adapter.Locked) (adapter.Change, error) {
	return adapter.Change{Changed: true}, nil
}
func (missingRefreshAdapter) Unwire(context.Context, string, adapter.Dependency, adapter.Export) (adapter.Change, error) {
	return adapter.Change{}, nil
}
func (missingRefreshAdapter) Refresh(context.Context, string, adapter.Dependency, adapter.Export, adapter.Locked) error {
	requirement := adapter.ToolRequirement{Ecosystem: "demo", Command: "demo", Install: "https://example.test/install"}
	return adapter.MissingToolError{Requirement: requirement}
}
func (missingRefreshAdapter) Drift(context.Context, string, adapter.Dependency, adapter.Export, adapter.Locked) ([]adapter.Finding, error) {
	return nil, nil
}

func TestWireWarnsAndSucceedsWhenRefreshToolIsMissing(t *testing.T) {
	module := &manifest.Manifest{Module: manifest.Module{Exports: []manifest.Export{{Ecosystem: "demo", Name: "demo"}}}}
	outcomes, err := wireAllUsing(context.Background(), t.TempDir(), manifest.Dependency{}, module, manifest.LockedDependency{}, true, []adapter.Adapter{missingRefreshAdapter{}})
	if err != nil || len(outcomes) != 1 || !strings.Contains(outcomes[0].Warning, "demo: demo not found — install:") {
		t.Fatalf("outcomes=%+v err=%v", outcomes, err)
	}
}
