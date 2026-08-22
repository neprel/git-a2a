package pypi

import (
	"context"
	"github.com/neprel/git-a2a/internal/adapter"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWireGoldenIdempotentUnwire(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join("..", "..", "testdata", "consumer-uv")
	original, err := os.ReadFile(filepath.Join(fixture, "pyproject.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "pyproject.toml"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "uv.lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{Git: "https://github.com/acme/lib-utils.git", Ref: "main", Track: "locked"}
	exp := adapter.Export{Ecosystem: "pypi", Name: "acme-lib-utils"}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	a := Adapter{}
	change, err := a.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed {
		t.Fatalf("wire: %#v %v", change, err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "pyproject.toml"))
	want, _ := os.ReadFile(filepath.Join(fixture, "pyproject.golden.toml"))
	if string(got) != string(want) {
		t.Fatalf("golden differs\ngot:\n%s\nwant:\n%s", got, want)
	}
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
	got, _ = os.ReadFile(filepath.Join(root, "pyproject.toml"))
	if string(got) != string(original) {
		t.Fatalf("unwire differs\ngot:\n%s\nwant:\n%s", got, original)
	}
}

func TestDriftMissingEntryIsUnwired(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte("[project]\nname = \"consumer\"\ndependencies = []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err := (Adapter{}).Drift(context.Background(), root,
		adapter.Dependency{Git: "https://github.com/acme/lib.git"},
		adapter.Export{Ecosystem: "pypi", Name: "acme-lib"},
		adapter.Locked{Git: "https://github.com/acme/lib.git", Commit: strings.Repeat("a", 40)})
	if err != nil || len(findings) != 1 {
		t.Fatalf("findings=%v err=%v", findings, err)
	}
}

func TestWireConvertsInlineDependencyArraysOnly(t *testing.T) {
	for _, initial := range []string{
		"[project]\nname = \"consumer\"\ndependencies = []\n[tool.other]\nitems = []\n",
		"[project]\nname = \"consumer\"\ndependencies = [\"click>=8\", \"rich\"]\n[tool.other]\nitems = []\n",
	} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(initial), 0o644); err != nil {
			t.Fatal(err)
		}
		dep := adapter.Dependency{Git: "https://github.com/acme/lib.git", Ref: "main", Track: "locked"}
		exp := adapter.Export{Ecosystem: "pypi", Name: "acme-lib"}
		locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
		if _, err := (Adapter{}).Wire(context.Background(), root, dep, exp, locked); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(filepath.Join(root, "pyproject.toml"))
		if !strings.Contains(string(got), "dependencies = [\n  \"") || !strings.Contains(string(got), "acme-lib @ git+") {
			t.Fatalf("inline array not converted:\n%s", got)
		}
		if !strings.Contains(string(got), "[tool.other]\nitems = []") {
			t.Fatalf("unrelated array changed:\n%s", got)
		}
	}
}

func TestWireUpdatesExistingPEP621GitPin(t *testing.T) {
	root := t.TempDir()
	oldCommit := strings.Repeat("a", 40)
	newCommit := strings.Repeat("b", 40)
	content := "[project]\nname = \"consumer\"\ndependencies = [\n  \"left-pad>=1\",\n  \"acme-lib @ git+https://example.test/acme/lib.git@" + oldCommit + "\",\n]\n"
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{Git: "https://mirror.example.test/acme/lib.git", Track: "locked"}
	exp := adapter.Export{Ecosystem: "pypi", Name: "acme-lib"}
	change, err := (Adapter{}).Wire(context.Background(), root, dep, exp, adapter.Locked{Git: dep.Git, Commit: newCommit})
	if err != nil || !change.Changed {
		t.Fatalf("change=%#v err=%v", change, err)
	}
	updated, readErr := os.ReadFile(filepath.Join(root, "pyproject.toml"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	got := string(updated)
	if strings.Contains(got, oldCommit) || !strings.Contains(got, "git+"+dep.Git+"@"+newCommit) || !strings.Contains(got, "left-pad>=1") {
		t.Fatalf("dependency was not updated minimally:\n%s", got)
	}
}

func TestWireUpdatesBareUVSourceWithoutDuplicatingOrJoiningHeader(t *testing.T) {
	root := t.TempDir()
	oldCommit := strings.Repeat("a", 40)
	newCommit := strings.Repeat("b", 40)
	content := "[project]\nname = \"consumer\"\ndependencies = [\"acme_lib\"]\n\n[tool.uv.sources]\nacme_lib = { git = \"https://example.test/acme/lib.git\", rev = \"" + oldCommit + "\" }\n\n[tool.other]\nkeep = true\n"
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "uv.lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{Git: "https://example.test/acme/lib.git", Track: "locked"}
	exp := adapter.Export{Ecosystem: "pypi", Name: "acme_lib"}
	if _, err := (Adapter{}).Wire(context.Background(), root, dep, exp, adapter.Locked{Git: dep.Git, Commit: newCommit}); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(filepath.Join(root, "pyproject.toml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(updated)
	if strings.Count(got, "rev =") != 1 || strings.Contains(got, oldCommit) || !strings.Contains(got, "[tool.uv.sources]\n\"acme_lib\" =") {
		t.Fatalf("UV source was not replaced minimally:\n%s", got)
	}
	if !strings.Contains(got, "\n[tool.other]\nkeep = true") {
		t.Fatalf("following table was changed:\n%s", got)
	}
	if findings, err := (Adapter{}).Drift(context.Background(), root, dep, exp, adapter.Locked{Git: dep.Git, Commit: newCommit}); err != nil || len(findings) != 0 {
		t.Fatalf("drift=%v err=%v", findings, err)
	}
}
