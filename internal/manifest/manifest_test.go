package manifest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExamples(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "spec", "examples", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			var err error
			if strings.HasSuffix(path, ".lock") {
				_, err = LoadLock(path)
			} else {
				_, err = Load(path)
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInvalidExamplesNamePath(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "spec", "examples", "invalid", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 4 {
		t.Fatalf("got %d invalid examples", len(paths))
	}
	for _, path := range paths {
		_, err := Load(path)
		if err == nil {
			t.Errorf("%s unexpectedly valid", path)
			continue
		}
		if !strings.Contains(err.Error(), ".") && !strings.Contains(err.Error(), "schema") {
			t.Errorf("%s error does not identify a path: %v", path, err)
		}
	}
}

func TestLockDeterministic(t *testing.T) {
	l := &Lock{Schema: 1, Dependencies: map[string]LockedDependency{
		"z": {Git: "u", Ref: "main", Path: ".", Commit: strings.Repeat("a", 40), Manifest: "sha256:" + strings.Repeat("b", 64)},
		"a": {Git: "u", Ref: "main", Path: ".", Commit: strings.Repeat("c", 40), Manifest: "sha256:" + strings.Repeat("d", 64)},
	}}
	one, err := MarshalLock(l)
	if err != nil {
		t.Fatal(err)
	}
	two, err := MarshalLock(l)
	if err != nil {
		t.Fatal(err)
	}
	if string(one) != string(two) {
		t.Fatal("lock rendering is not deterministic")
	}
	if strings.Index(string(one), "a:") > strings.Index(string(one), "z:") {
		t.Fatal("dependency keys are not sorted")
	}
	if !strings.HasPrefix(string(one), "schema: 1\ndependencies:\n") {
		t.Fatalf("non-canonical top-level order:\n%s", one)
	}
}

func TestManifestExtension(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "spec", "examples", "acme-lib-utils.a2amodule.yml"))
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, []byte("x-test: yes\n")...)
	if _, err := Parse(b); err != nil {
		t.Fatal(err)
	}
}

func TestSpecManifestExamplesAreCanonical(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "spec", "examples", "*.a2amodule.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		original, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		formatted, formatErr := Format(original)
		if formatErr != nil {
			t.Fatal(formatErr)
		}
		if !bytes.Equal(formatted, original) {
			t.Errorf("%s is not canonical", filepath.Base(path))
		}
	}
}

func TestUpdateDependenciesPreservesCommentsStylesAndExtensions(t *testing.T) {
	original := []byte(`# manifest comment
schema: 1
module:
  id: consumer
  description: >-
    folded text stays folded
  languages: [go, python]
x-verbatim: "quoted value"
dependencies:
  - id: dep
    git: https://example.test/old.git
    ref: main # ref comment
    track: locked
    wire: [npm, pypi]
    x-private: "keep quoted"
`)
	wire := []string{"npm", "pypi"}
	updated, err := UpdateDependencies(original, []Dependency{{ID: "dep", Git: "https://example.test/new.git", Ref: "release", Track: "locked", Wire: &wire}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(updated)
	for _, preserved := range []string{"# manifest comment", "description: >-", "languages: [go, python]", `x-verbatim: "quoted value"`, "ref: release # ref comment", "wire: [npm, pypi]", `x-private: "keep quoted"`} {
		if !strings.Contains(text, preserved) {
			t.Errorf("missing preserved text %q:\n%s", preserved, text)
		}
	}
}

func TestUnknownContactKindCarriesArbitraryKeysThroughJSON(t *testing.T) {
	raw := []byte("schema: 1\nmodule: {id: consumer}\nagents:\n  - name: owner\n    role: owner\n    contacts:\n      - intents: [incident]\n        kind: pager-duty\n        service: checkout\n        escalation: 2\n        x-color: red\n")
	m, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	contact := m.Agents[0].Contacts[0]
	if contact.Extensions["service"] != "checkout" || contact.Extensions["escalation"] != 2 {
		t.Fatalf("extensions = %#v", contact.Extensions)
	}
	encoded, err := json.Marshal(contact)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"service":"checkout"`, `"escalation":2`, `"x-color":"red"`} {
		if !bytes.Contains(encoded, []byte(field)) {
			t.Errorf("JSON missing %s: %s", field, encoded)
		}
	}
}

func TestKnownContactKindRejectsAnotherKindsKeys(t *testing.T) {
	raw := []byte("schema: 1\nmodule: {id: consumer}\nagents:\n  - name: owner\n    role: owner\n    contacts:\n      - intents: [question]\n        kind: email\n        address: owner@example.test\n        project: WRONG\n")
	if _, err := Parse(raw); err == nil || !strings.Contains(err.Error(), "project: not valid for contact kind email") {
		t.Fatalf("error = %v", err)
	}
}
