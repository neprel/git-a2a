package manifest

import (
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
