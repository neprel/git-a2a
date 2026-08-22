package composer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/adapter"
)

func TestWireGoldenIdempotentUnwire(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join("..", "..", "testdata", "consumer-composer")
	original := mustRead(t, filepath.Join(fixture, "composer.json"))
	if err := os.WriteFile(filepath.Join(root, "composer.json"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{Git: "https://github.com/acme/lib-utils.git", Ref: "main", Track: "locked"}
	exp := adapter.Export{Ecosystem: "composer", Name: "acme/lib-utils"}
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
