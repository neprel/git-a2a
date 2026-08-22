package hackage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/adapter"
)

func TestWireGoldenIdempotentUnwire(t *testing.T) {
	for _, tc := range []struct {
		fixture, file, golden string
	}{
		{"consumer-cabal", "cabal.project", "cabal.golden.project"},
		{"consumer-stack", "stack.yaml", "stack.golden.yaml"},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			root := t.TempDir()
			fixture := filepath.Join("..", "..", "testdata", tc.fixture)
			original := mustRead(t, filepath.Join(fixture, tc.file))
			if err := os.WriteFile(filepath.Join(root, tc.file), original, 0o644); err != nil {
				t.Fatal(err)
			}
			dep := adapter.Dependency{Git: "https://github.com/acme/lib-utils.git", Ref: "main", Track: "locked"}
			exp := adapter.Export{Ecosystem: "hackage", Name: "acme-lib-utils", Path: "haskell/lib"}
			locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
			a := Adapter{}
			change, err := a.Wire(context.Background(), root, dep, exp, locked)
			if err != nil || !change.Changed {
				t.Fatalf("wire=%#v err=%v", change, err)
			}
			if got, want := mustRead(t, filepath.Join(root, tc.file)), mustRead(t, filepath.Join(fixture, tc.golden)); string(got) != string(want) {
				t.Fatalf("golden differs\ngot:\n%s\nwant:\n%s", got, want)
			}
			if change, err = a.Wire(context.Background(), root, dep, exp, locked); err != nil || change.Changed {
				t.Fatalf("second wire=%#v err=%v", change, err)
			}
			if findings, err := a.Drift(context.Background(), root, dep, exp, locked); err != nil || len(findings) != 0 {
				t.Fatalf("drift=%v err=%v", findings, err)
			}
			if change, err = a.Unwire(context.Background(), root, dep, exp); err != nil || !change.Changed {
				t.Fatalf("unwire=%#v err=%v", change, err)
			}
			if got := mustRead(t, filepath.Join(root, tc.file)); string(got) != string(original) {
				t.Fatalf("unwire differs\ngot:\n%s\nwant:\n%s", got, original)
			}
		})
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
