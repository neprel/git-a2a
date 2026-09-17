package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMinimalSchema2(t *testing.T) {
	m, err := Parse([]byte("schema: 2\ncomponent:\n  id: demo\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Component.ID != "demo" || m.Agent != nil {
		t.Fatalf("%+v", m)
	}
}

func TestSchema1RejectedWithMigration(t *testing.T) {
	_, err := Parse([]byte("schema: 1\nmodule:\n  id: old\n"))
	var schemaErr *UnsupportedSchemaError
	if !errors.As(err, &schemaErr) || !strings.Contains(err.Error(), "migration-v2") {
		t.Fatalf("%v", err)
	}
}

func TestUnknownFieldsRejected(t *testing.T) {
	_, err := Parse([]byte("schema: 2\ncomponent:\n  id: demo\n  roles: []\n"))
	if err == nil || !strings.Contains(err.Error(), "roles") {
		t.Fatalf("%v", err)
	}
}

func TestPublishedDependencyShape(t *testing.T) {
	body := []byte(`schema: 2
component:
  id: app
agent:
  card: https://agents.example/app/card.json
dependencies:
  - name: lib
    git: https://example.test/lib.git
    ref: main
    bindings:
      - adapter: npm
        variant: pnpm
        export: '@example/lib'
`)
	m, err := Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	if m.Dependencies[0].Bindings[0].Variant != "pnpm" {
		t.Fatalf("%+v", m)
	}
}

func TestUnsafePathsRejected(t *testing.T) {
	for _, field := range []string{"surface", "export", "dependency", "binding"} {
		m := &Manifest{Schema: 2, Component: Component{ID: "demo"}}
		switch field {
		case "surface":
			m.Component.Surface = "../secret"
		case "export":
			m.Component.Exports = []Export{{Adapter: "npm", Name: "x", Path: "../x"}}
		case "dependency":
			m.Dependencies = []Dependency{{Name: "x", Git: "x", Path: "../x"}}
		case "binding":
			m.Dependencies = []Dependency{{Name: "x", Git: "x", Bindings: []Binding{{Adapter: "npm", Variant: "npm", Path: "../x"}}}}
		}
		if err := m.Validate(); err == nil {
			t.Errorf("%s accepted", field)
		}
	}
}

func TestBothManifestSpellingsRejected(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{CanonicalName, AlternateName} {
		if err := os.WriteFile(filepath.Join(root, n), []byte("schema: 2\ncomponent: {id: x}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Path(root); err == nil {
		t.Fatal("both spellings accepted")
	}
}

func TestLockRequiresAppliedAgent(t *testing.T) {
	l := &Lock{Schema: 2, Dependencies: map[string]LockedDependency{"x": {Component: "x", Git: "g", Ref: "main", Commit: strings.Repeat("a", 40), Manifest: "sha256:" + strings.Repeat("b", 64), Bindings: []Binding{{Adapter: "npm", Variant: "npm"}}}}}
	if err := l.Validate(); err == nil || !strings.Contains(err.Error(), "agent.declaredCard") {
		t.Fatalf("%v", err)
	}
}

func TestLockRequiresExactCommitAndManifestDigest(t *testing.T) {
	l := &Lock{Schema: 2, Dependencies: map[string]LockedDependency{"x": {Component: "x", Git: "g", Ref: "main", Commit: "main", Manifest: "none", Agent: LockedAgent{DeclaredCard: "https://agents.example/x", Card: "https://agents.example/x", Commit: "main"}}}}
	err := l.Validate()
	if err == nil || !strings.Contains(err.Error(), "40-character") || !strings.Contains(err.Error(), "sha256 digest") {
		t.Fatalf("%v", err)
	}
}

func TestLockRequiresUsableAgentReferenceAtAppliedCommit(t *testing.T) {
	commit := strings.Repeat("a", 40)
	base := LockedDependency{Component: "x", Git: "g", Ref: "main", Commit: commit, Manifest: "sha256:" + strings.Repeat("b", 64)}
	tests := []struct {
		name  string
		agent LockedAgent
		want  string
	}{
		{"https changed", LockedAgent{DeclaredCard: "https://agents.example/x", Card: "local.json", Commit: commit}, "retain the declared"},
		{"relative wrong target", LockedAgent{DeclaredCard: "card.json", Card: "card.json", Commit: commit}, "must resolve to .git-a2a/agents/x/agent-card.json"},
		{"wrong provenance", LockedAgent{DeclaredCard: "card.json", Card: ".git-a2a/agents/x/agent-card.json", Commit: strings.Repeat("c", 40)}, "must equal dependency commit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := base
			d.Agent = tt.agent
			err := (&Lock{Schema: 2, Dependencies: map[string]LockedDependency{"x": d}}).Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestBindingsMayShareExportAcrossConcreteVariants(t *testing.T) {
	m := &Manifest{Schema: 2, Component: Component{ID: "demo"}, Dependencies: []Dependency{{
		Name: "lib", Git: "g", Bindings: []Binding{
			{Adapter: "maven", Variant: "gradle-kts", Export: "org.example:lib"},
			{Adapter: "maven", Variant: "maven", Export: "org.example:lib"},
		},
	}}}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDependenciesCannotClaimSameConcreteBinding(t *testing.T) {
	m := &Manifest{Schema: 2, Component: Component{ID: "demo"}, Dependencies: []Dependency{
		{Name: "one", Git: "g1", Bindings: []Binding{{Adapter: "npm", Variant: "npm", Export: "pkg"}}},
		{Name: "two", Git: "g2", Bindings: []Binding{{Adapter: "npm", Variant: "npm", Export: "pkg"}}},
	}}
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "already claimed") {
		t.Fatalf("%v", err)
	}
}
