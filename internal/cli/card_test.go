package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/a2a"
	"github.com/neprel/git-a2a/internal/manifest"
)

func TestCardExportSynthesizesValidV1Card(t *testing.T) {
	root := t.TempDir()
	manifestBytes := []byte("schema: 1\nmodule:\n  id: demo\n  description: Demo module owner.\n  repository: https://github.com/acme/demo.git\nagents:\n  - name: demo-owner\n    role: owner\n    contacts:\n      - intents: [question]\n        kind: a2a\n        url: https://agents.acme.example/demo\n")
	if err := os.WriteFile(filepath.Join(root, "a2amodule.yml"), manifestBytes, 0o644); err != nil {
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

func TestCardExportStripsRepositoryURLUserinfo(t *testing.T) {
	for _, test := range []struct {
		name       string
		repository string
		remote     string
	}{
		{name: "manifest repository", repository: "https://user:TOKEN@example.test/acme/lib.git"},
		{name: "git remote", remote: "https://user:TOKEN@example.test/acme/lib.git"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			repositoryLine := ""
			if test.repository != "" {
				repositoryLine = "  repository: " + test.repository + "\n"
			}
			manifest := "schema: 1\nmodule:\n  id: acme-lib\n" + repositoryLine + "agents:\n  - name: owner\n    role: owner\n    contacts:\n      - intents: [question]\n        kind: a2a\n        url: https://agent.example.test/\n"
			if err := os.WriteFile(filepath.Join(root, "a2amodule.yml"), []byte(manifest), 0o644); err != nil {
				t.Fatal(err)
			}
			if test.remote != "" {
				runGitForCard(t, root, "init")
				runGitForCard(t, root, "remote", "add", "origin", test.remote)
			}
			var out, errOut bytes.Buffer
			app := New(&out, &errOut)
			app.Root = root
			if code := app.Run([]string{"card", "export", "owner"}); code != 0 {
				t.Fatalf("exit %d: %s", code, errOut.String())
			}
			if strings.Contains(out.String(), "user") || strings.Contains(out.String(), "TOKEN") || !strings.Contains(out.String(), "https://example.test/acme/lib.git") {
				t.Fatalf("userinfo leaked:\n%s", out.String())
			}
		})
	}
}

func runGitForCard(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
