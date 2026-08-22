package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
