package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWhoRendersContactBudgetInHumanAndJSON(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, ".git-a2a", "cache", "acme-lib")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `schema: 1
module: {id: acme-lib}
agents:
  - name: acme-owner
    role: owner
    contacts: [{intents: [question], kind: email, address: owner@example.com}]
policy:
  contact-budget: {per-consumer-daily: "2", note: Coordinate bursts.}
`
	if err := os.WriteFile(filepath.Join(cache, "a2amodule.yml"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"who", "acme-lib"}, {"who", "acme-lib", "--json"}} {
		var out, errOut bytes.Buffer
		app := New(&out, &errOut)
		app.Root = root
		if code := app.Run(args); code != 0 {
			t.Fatalf("args=%v exit=%d stderr=%s", args, code, errOut.String())
		}
		if !strings.Contains(out.String(), "per-consumer-daily") || !strings.Contains(out.String(), "2") {
			t.Fatalf("args=%v output=%s", args, out.String())
		}
	}
}
