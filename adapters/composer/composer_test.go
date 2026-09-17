package composer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/v2/internal/adapter"
)

func TestWireGoldenIdempotentUnwire(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join("..", "..", "testdata", "consumer-composer")
	original := mustRead(t, filepath.Join(fixture, "composer.json"))
	if err := os.WriteFile(filepath.Join(root, "composer.json"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{Name: "acme-lib-utils", Git: "https://github.com/acme/lib-utils.git", Ref: "main"}
	exp := adapter.Export{Adapter: "composer", Name: "acme/lib-utils"}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	a := Adapter{}
	change, err := a.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed {
		t.Fatalf("wire=%#v err=%v", change, err)
	}
	assertJSONEqual(t, mustRead(t, filepath.Join(root, "composer.json")), mustRead(t, filepath.Join(fixture, "composer.golden.json")))
	if change, err = a.Wire(context.Background(), root, dep, exp, locked); err != nil || change.Changed {
		t.Fatalf("second wire=%#v err=%v", change, err)
	}
	if findings, err := a.Drift(context.Background(), root, dep, exp, locked); err != nil || len(findings) != 0 {
		t.Fatalf("drift=%v err=%v", findings, err)
	}
	if change, err = a.Unwire(context.Background(), root, dep, exp); err != nil || !change.Changed {
		t.Fatalf("unwire=%#v err=%v", change, err)
	}
	assertJSONEqual(t, mustRead(t, filepath.Join(root, "composer.json")), original)
}

func TestSubdirectoryIsNotWirable(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "composer.json"), []byte("{\"name\":\"acme/consumer\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{Name: "acme-lib", Git: "https://github.com/acme/lib.git"}
	exp := adapter.Export{Adapter: "composer", Name: "acme/lib", Path: "php"}
	before := mustRead(t, filepath.Join(root, "composer.json"))
	err := (Adapter{}).Capability(root, dep, exp)
	if !adapter.IsNotWirable(err) {
		t.Fatalf("err=%v, want typed not-wirable", err)
	}
	if got := mustRead(t, filepath.Join(root, "composer.json")); string(got) != string(before) {
		t.Fatal("Capability mutated composer.json")
	}
}

func TestPullChecksToolBeforeMutation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "composer.json")
	original := []byte("{\"name\":\"acme/consumer\"}\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	dep := adapter.Dependency{Git: "https://example.com/acme/lib.git", Ref: "main"}
	exp := adapter.Export{Name: "acme/lib"}
	_, err := (Adapter{}).Pull(context.Background(), root, dep, exp, adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)})
	if !adapter.IsMissingTool(err) {
		t.Fatalf("err=%v, want missing-tool", err)
	}
	if got := mustRead(t, path); string(got) != string(original) {
		t.Fatal("Pull mutated composer.json before tool preflight")
	}
}

func TestRemoveUsesComposerSupportedDependencyFlag(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "composer.json")
	body := []byte("{\"name\":\"acme/consumer\",\"repositories\":{\"acme/lib\":{\"type\":\"vcs\",\"url\":\"https://example.com/acme/lib.git\"}},\"require\":{\"acme/lib\":\"dev-main#aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"}}\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	log := filepath.Join(root, "composer.log")
	writeCommandStub(t, bin, "composer", log)
	t.Setenv("PATH", bin)
	dep := adapter.Dependency{Git: "https://example.com/acme/lib.git", Ref: "main"}
	exp := adapter.Export{Name: "acme/lib"}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	if _, err := (Adapter{}).Remove(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatal(err)
	}
	got := string(mustRead(t, log))
	if !strings.Contains(got, "remove acme/lib --update-with-dependencies ") {
		t.Fatalf("composer command = %q", got)
	}
}

func writeCommandStub(t *testing.T, bin, name, log string) {
	t.Helper()
	path := filepath.Join(bin, name)
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$COMMAND_LOG\"\n"
	mode := os.FileMode(0o755)
	if runtime.GOOS == "windows" {
		path += ".cmd"
		body = "@echo off\r\n>>\"%COMMAND_LOG%\" echo %*\r\n"
		mode = 0o644
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMMAND_LOG", log)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func assertJSONEqual(t *testing.T, got, want []byte) {
	t.Helper()
	var a, b any
	if json.Unmarshal(got, &a) != nil || json.Unmarshal(want, &b) != nil {
		t.Fatalf("invalid JSON\ngot %s\nwant %s", got, want)
	}
	ga, _ := json.Marshal(a)
	gb, _ := json.Marshal(b)
	if string(ga) != string(gb) {
		t.Fatalf("JSON differs\ngot %s\nwant %s", got, want)
	}
}
