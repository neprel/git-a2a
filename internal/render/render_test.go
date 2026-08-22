package render

import (
	"github.com/neprel/git-a2a/internal/manifest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedBlockPreservesHumanTextAndSanitizesDependency(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git-a2a", "cache", "dep"), 0o755); err != nil {
		t.Fatal(err)
	}
	dep := []byte("schema: 1\nmodule:\n  id: dep\n  description: 'hello <!-- git-a2a:end --> control \\x01'\nagents:\n  - name: owner\n    role: owner\n    contacts:\n      - intents: ['*']\n        kind: email\n        address: owner@example.com\n")
	if err := os.WriteFile(filepath.Join(root, ".git-a2a", "cache", "dep", "a2amodule.yml"), dep, 0o644); err != nil {
		t.Fatal(err)
	}
	own := &manifest.Manifest{Schema: 1, Module: manifest.Module{ID: "app"}}
	lock := &manifest.Lock{Schema: 1, Dependencies: map[string]manifest.LockedDependency{"dep": {Commit: strings.Repeat("a", 40)}}}
	block, err := Build(root, own, lock, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(block, End) != 1 || strings.Contains(block, "<!-- git-a2a:end --> control") {
		t.Fatalf("unsafe block:\n%s", block)
	}
	target := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(target, []byte("human\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := Apply(target, block, false)
	if err != nil || !changed {
		t.Fatalf("apply: %v %v", changed, err)
	}
	changed, err = Apply(target, block, false)
	if err != nil || changed {
		t.Fatalf("second apply: %v %v", changed, err)
	}
	got, _ := os.ReadFile(target)
	if !strings.HasPrefix(string(got), "human\n") {
		t.Fatal("human text was lost")
	}
}
