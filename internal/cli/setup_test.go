package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupDryRunInstallAndCheck(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	for _, marker := range []string{".claude", ".codex", ".cursor", filepath.Join(".github", "agents"), ".gemini", ".opencode"} {
		if err := os.MkdirAll(filepath.Join(root, marker), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Existing guidance\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const existingMCP = "{\n    \"other\" : true\n}\n"
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(existingMCP), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root, app.Home = root, home
	if code := app.Run([]string{"setup", "--dry-run"}); code != 0 {
		t.Fatalf("dry-run exit %d: %s", code, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".agents")); !os.IsNotExist(err) {
		t.Fatalf("dry-run mutated skill dir: %v", err)
	}
	if !strings.Contains(out.String(), "would write .agents/skills/git-a2a/SKILL.md") {
		t.Fatalf("dry-run output:\n%s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"setup"}); code != 0 {
		t.Fatalf("setup exit %d: %s", code, errOut.String())
	}
	for _, path := range []string{
		".agents/skills/git-a2a/SKILL.md", ".claude/skills/git-a2a/SKILL.md", ".mcp.json",
		".cursor/mcp.json", ".vscode/mcp.json", ".gemini/settings.json", ".codex/config.toml", "opencode.json",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
	agents, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if !strings.HasPrefix(string(agents), "# Existing guidance\n") || strings.Count(string(agents), setupBegin) != 1 {
		t.Fatalf("AGENTS.md:\n%s", agents)
	}
	var claude map[string]any
	body, _ := os.ReadFile(filepath.Join(root, ".mcp.json"))
	if !strings.Contains(string(body), "    \"other\" : true") {
		t.Fatalf("setup rewrote unrelated JSON bytes:\n%s", body)
	}
	if err := json.Unmarshal(body, &claude); err != nil {
		t.Fatal(err)
	}
	if claude["other"] != true || claude["mcpServers"].(map[string]any)["git-a2a"] == nil {
		t.Fatalf("Claude config = %#v", claude)
	}

	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"setup", "--check"}); code != 0 {
		t.Fatalf("check exit %d: %s\n%s", code, errOut.String(), out.String())
	}
	if !strings.Contains(errOut.String(), "agent guidance is current") {
		t.Fatalf("check verdict: %s", errOut.String())
	}
	if code := app.Run([]string{"setup"}); code != 0 {
		t.Fatalf("idempotent setup exit %d", code)
	}
	again, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if !bytes.Equal(agents, again) {
		t.Fatal("second setup changed AGENTS.md")
	}
}

func TestSetupCheckReportsDriftWithoutMutation(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root, app.Home = root, home
	if code := app.Run([]string{"setup", "--check"}); code != 1 {
		t.Fatalf("check exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("check mutated repository: %v", err)
	}
}
