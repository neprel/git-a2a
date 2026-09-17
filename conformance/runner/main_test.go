package main

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestReadCommandsReplacesTokensAfterJSONDecode(t *testing.T) {
	path := t.TempDir() + string(os.PathSeparator) + "command"
	data := `[["init","--id","acme-app"],["add","<CORPUS_ROOT>/fixture","--name","lib"]]`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readCommands(path, map[string]string{"<CORPUS_ROOT>": `D:\a\git-a2a`})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"init", "--id", "acme-app"}, {"add", `D:\a\git-a2a/fixture`, "--name", "lib"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}
}

func TestReadCommandsRejectsOldSingleInvocationProtocol(t *testing.T) {
	path := t.TempDir() + string(os.PathSeparator) + "command"
	if err := os.WriteFile(path, []byte(`["list"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readCommands(path, nil); err == nil || !strings.Contains(err.Error(), "array of argv arrays") {
		t.Fatalf("error = %v", err)
	}
}

func TestMergeEnvironmentAppendsInheritedPath(t *testing.T) {
	got := mergeEnvironment([]string{"PATH=/system", "A=old"}, map[string]string{"PATH": "/case" + string(os.PathListSeparator), "A": "new"})
	want := []string{"A=new", "PATH=/case" + string(os.PathListSeparator) + "/system"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %#v, want %#v", got, want)
	}
}
