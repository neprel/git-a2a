package main

import (
	"os"
	"reflect"
	"testing"
)

func TestReadCommandReplacesWindowsPathAfterJSONDecode(t *testing.T) {
	path := t.TempDir() + string(os.PathSeparator) + "command"
	if err := os.WriteFile(path, []byte(`["validate","<CORPUS_ROOT>/spec/example.yml"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readCommand(path, map[string]string{"<CORPUS_ROOT>": `D:\a\git-a2a`})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"validate", `D:\a\git-a2a/spec/example.yml`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command = %#v, want %#v", got, want)
	}
}

func TestMergeEnvironmentAppendsInheritedPath(t *testing.T) {
	got := mergeEnvironment([]string{"PATH=/system", "A=old"}, map[string]string{"PATH": "/case" + string(os.PathListSeparator), "A": "new"})
	want := []string{"A=new", "PATH=/case" + string(os.PathListSeparator) + "/system"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %#v, want %#v", got, want)
	}
}
