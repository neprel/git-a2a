package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/catalog"
)

func TestCatalogExportMatchesARDGolden(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "catalog")
	want, err := os.ReadFile(filepath.Join(root, "ai-catalog.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"catalog", "export"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if got, want := errOut.String(), "exported 2 A2A catalog entries\n"; got != want {
		t.Fatalf("catalog summary = %q, want %q", got, want)
	}
	if !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("catalog differs from golden:\n%s", out.String())
	}
	var value catalog.Catalog
	if err = json.Unmarshal(out.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if err = catalog.Validate(&value); err != nil {
		t.Fatalf("published ARD shape: %v", err)
	}
	for _, entry := range value.Entries {
		if entry.Type != catalog.A2AAgentCardType {
			t.Fatalf("entry type = %q", entry.Type)
		}
	}
}

func TestEntrySummariesMatchGolden(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "entry-summaries.golden.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join([]string{
		wireEntrySummary(1),
		wireEntrySummary(2),
		catalogEntrySummary(1),
		catalogEntrySummary(2),
	}, "\n") + "\n"
	if got != string(want) {
		t.Fatalf("entry summaries differ from golden:\n%s", got)
	}
}

func TestCatalogExportUsesDefaultBranchAndNeverHEAD(t *testing.T) {
	root := t.TempDir()
	manifest := "schema: 1\nmodule:\n  id: demo\n  repository: https://example.test/acme/demo.git\nagents:\n  - name: owner\n    role: owner\n    contacts:\n      - intents: [question]\n        kind: a2a\n        url: https://agent.example.test/\n"
	if err := os.WriteFile(filepath.Join(root, "a2amodule.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	app.Runner = scriptedGitRunner{output: []byte("ref: refs/heads/main\tHEAD\n2222222222222222222222222222222222222222\tHEAD\n")}
	if code := app.Run([]string{"catalog", "export"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"ref": "main"`) || strings.Contains(out.String(), `"ref": "HEAD"`) {
		t.Fatalf("catalog ref was not resolved:\n%s", out.String())
	}

	out.Reset()
	errOut.Reset()
	app.Runner = scriptedGitRunner{err: os.ErrNotExist}
	if code := app.Run([]string{"catalog", "export"}); code != 0 {
		t.Fatalf("unresolved default exit %d: %s", code, errOut.String())
	}
	if strings.Contains(out.String(), `"ref"`) {
		t.Fatalf("unresolved default ref must be omitted:\n%s", out.String())
	}
}
