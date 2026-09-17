package adapters_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/adapters/cargo"
	"github.com/neprel/git-a2a/adapters/clojure"
	"github.com/neprel/git-a2a/adapters/composer"
	"github.com/neprel/git-a2a/adapters/gem"
	"github.com/neprel/git-a2a/adapters/golang"
	"github.com/neprel/git-a2a/adapters/hackage"
	"github.com/neprel/git-a2a/adapters/hex"
	"github.com/neprel/git-a2a/adapters/nix"
	"github.com/neprel/git-a2a/adapters/npm"
	pubadapter "github.com/neprel/git-a2a/adapters/pub"
	"github.com/neprel/git-a2a/adapters/pypi"
	"github.com/neprel/git-a2a/adapters/swift"
	"github.com/neprel/git-a2a/adapters/zig"
	"github.com/neprel/git-a2a/internal/adapter"
)

func TestPublicManifestCorpusEditorsRoundTrip(t *testing.T) {
	commit := strings.Repeat("a", 40)
	dep := adapter.Dependency{Name: "corpus", Git: "https://github.com/acme/git-a2a-corpus.git", Ref: "main"}
	locked := adapter.Locked{Git: dep.Git, Ref: "refs/heads/main", Commit: commit}
	goAdapter := golang.Adapter{ResolveVersion: func(context.Context, string, string, string) (string, error) {
		return "v0.0.0-20260822000000-aaaaaaaaaaaa", nil
	}}
	cases := map[string]struct {
		implementation manifestEditor
		export         adapter.Export
	}{
		"npm":      {npm.Adapter{}, adapter.Export{Adapter: "npm", Name: "@git-a2a/corpus-dependency"}},
		"pypi":     {pypi.Adapter{}, adapter.Export{Adapter: "pypi", Name: "git-a2a-corpus-dependency"}},
		"golang":   {goAdapter, adapter.Export{Adapter: "golang", Name: "github.com/acme/git-a2a-corpus"}},
		"cargo":    {cargo.Adapter{}, adapter.Export{Adapter: "cargo", Name: "git-a2a-corpus-dependency"}},
		"swift":    {swift.Adapter{}, adapter.Export{Adapter: "swift", Name: "GitA2ACorpusDependency"}},
		"pub":      {pubadapter.Adapter{}, adapter.Export{Adapter: "pub", Name: "git_a2a_corpus_dependency"}},
		"gem":      {gem.Adapter{}, adapter.Export{Adapter: "gem", Name: "git-a2a-corpus-dependency"}},
		"composer": {composer.Adapter{}, adapter.Export{Adapter: "composer", Name: "git-a2a/corpus-dependency"}},
		"hex":      {hex.Adapter{}, adapter.Export{Adapter: "hex", Name: "git_a2a_corpus_dependency"}},
		"hackage":  {hackage.Adapter{}, adapter.Export{Adapter: "hackage", Name: "git-a2a-corpus-dependency"}},
		"zig":      {zig.Adapter{}, adapter.Export{Adapter: "zig", Name: "git_a2a_corpus_dependency", Checksum: "1220" + strings.Repeat("b", 64)}},
		"clojure":  {clojure.Adapter{}, adapter.Export{Adapter: "clojure", Name: "git-a2a/corpus-dependency"}},
		"nix":      {nix.Adapter{}, adapter.Export{Adapter: "nix", Name: "git-a2a-corpus-dependency"}},
	}
	for ecosystem, test := range cases {
		ecosystem, test := ecosystem, test
		t.Run(ecosystem, func(t *testing.T) {
			matches, err := filepath.Glob(filepath.Join("..", "testdata", "corpus", ecosystem, "*", "*"))
			if err != nil || len(matches) < 10 {
				t.Fatalf("corpus entries=%d err=%v, want at least 10", len(matches), err)
			}
			for _, source := range matches {
				source := source
				t.Run(filepath.Base(filepath.Dir(source)), func(t *testing.T) {
					original, err := os.ReadFile(source)
					if err != nil {
						t.Fatal(err)
					}
					root := t.TempDir()
					target := filepath.Join(root, filepath.Base(source))
					if err = os.WriteFile(target, original, 0o644); err != nil {
						t.Fatal(err)
					}
					change, err := test.implementation.Wire(context.Background(), root, dep, test.export, locked)
					if err != nil || !change.Changed {
						t.Fatalf("Wire: change=%#v err=%v", change, err)
					}
					if change, err = test.implementation.Wire(context.Background(), root, dep, test.export, locked); err != nil || change.Changed {
						t.Fatalf("second Wire: change=%#v err=%v", change, err)
					}
					if findings, driftErr := test.implementation.Drift(context.Background(), root, dep, test.export, locked); driftErr != nil || len(findings) != 0 {
						t.Fatalf("Drift: findings=%v err=%v", findings, driftErr)
					}
					if change, err = test.implementation.Unwire(context.Background(), root, dep, test.export); err != nil || !change.Changed {
						t.Fatalf("Unwire: change=%#v err=%v", change, err)
					}
					restored, err := os.ReadFile(target)
					if err != nil || !bytes.Equal(restored, original) {
						t.Fatalf("Unwire did not restore original bytes (err=%v): %s", err, byteDifference(restored, original))
					}
				})
			}
		})
	}
}

// manifestEditor is the narrow editor surface exercised by the corpus.
// Product lifecycle goes exclusively through adapter.Adapter.
type manifestEditor interface {
	Wire(context.Context, string, adapter.Dependency, adapter.Export, adapter.Locked) (adapter.Change, error)
	Unwire(context.Context, string, adapter.Dependency, adapter.Export) (adapter.Change, error)
	Drift(context.Context, string, adapter.Dependency, adapter.Export, adapter.Locked) ([]adapter.Finding, error)
}

func byteDifference(got, want []byte) string {
	at := 0
	for at < len(got) && at < len(want) && got[at] == want[at] {
		at++
	}
	from := at - 40
	if from < 0 {
		from = 0
	}
	gotEnd, wantEnd := at+80, at+80
	if gotEnd > len(got) {
		gotEnd = len(got)
	}
	if wantEnd > len(want) {
		wantEnd = len(want)
	}
	return fmt.Sprintf("first difference at byte %d (lengths %d/%d)\ngot:  %q\nwant: %q", at, len(got), len(want), got[from:gotEnd], want[from:wantEnd])
}
