package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/neprel/git-a2a/internal/a2a"
	"github.com/neprel/git-a2a/internal/manifest"
)

func TestCardExportSynthesizesValidV1Card(t *testing.T) {
	root := t.TempDir()
	manifest := []byte("schema: 1\nmodule:\n  id: demo\n  description: Demo module owner.\n  repository: https://github.com/acme/demo.git\nagents:\n  - name: demo-owner\n    role: owner\n    contacts:\n      - intents: [question]\n        kind: a2a\n        url: https://agents.acme.example/demo\n")
	if err := os.WriteFile(filepath.Join(root, "a2amodule.yml"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"card", "export", "demo-owner"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var card map[string]any
	if json.Unmarshal(out.Bytes(), &card) != nil {
		t.Fatal("invalid json")
	}
	if err := a2a.Validate(card); err != nil {
		t.Fatal(err)
	}
	caps := card["capabilities"].(map[string]any)
	extensions := caps["extensions"].([]any)
	if extensions[0].(map[string]any)["uri"] != a2a.ExtensionURI {
		t.Fatalf("extension: %#v", extensions)
	}
}

func TestCheckAgentsUsesRelativeCardSnapshot(t *testing.T) {
	root := t.TempDir()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "cards", "v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, a2a.FileName("acme-lib-utils")), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	m := &manifest.Manifest{Agents: []manifest.Agent{{Name: "acme-lib-utils", Card: "cards/agent.json"}}}
	state, failed, details := checkAgents(m, map[string]string{"acme-lib-utils": "sha256:" + hex.EncodeToString(sum[:])}, root, false)
	if failed || state != "1 up" || len(details) != 0 {
		t.Fatalf("state=%q failed=%v details=%v", state, failed, details)
	}
}
