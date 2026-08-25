package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/manifest"
)

func TestAuthoringHelpersBuildValidManifest(t *testing.T) {
	root := t.TempDir()
	writeAuthorManifest(t, root)
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"sync"}); code != 0 {
		t.Fatalf("sync exit %d: %s", code, errOut.String())
	}
	commands := [][]string{
		{"agent", "add", "acme-owner", "--role", "owner", "--scope", "src/**", "--card", "https://agent.example/.well-known/agent-card.json", "--contact", "intents=question|change,kind=github-issue,repo=acme/example,labels=from-agent|change-request"},
		{"export", "add", "npm", "@acme/example", "--path", "packages/js"},
		{"policy", "set", "question=owner", "change=owner", "--may", "read-surface,ask", "--may-not", "commit,release", "--notes", "Ask the owner first."},
	}
	for _, args := range commands {
		out.Reset()
		errOut.Reset()
		if code := app.Run(args); code != 0 {
			t.Fatalf("%v exit %d: %s", args, code, errOut.String())
		}
	}
	m, err := manifest.LoadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Agents) != 1 || m.Agents[0].Name != "acme-owner" || len(m.Agents[0].Contacts) != 1 {
		t.Fatalf("agents = %#v", m.Agents)
	}
	if len(m.Module.Exports) != 1 || m.Module.Exports[0].Path != "packages/js" {
		t.Fatalf("exports = %#v", m.Module.Exports)
	}
	if m.Policy == nil || m.Policy.Intents["question"] != "owner" {
		t.Fatalf("policy = %#v", m.Policy)
	}
	if got := strings.Join(m.Policy.Consumers.May, ","); got != "read-surface,ask" || strings.Join(m.Policy.Consumers.MayNot, ",") != "commit,release" || m.Policy.Notes != "Ask the owner first." {
		t.Fatalf("consumer policy = %#v", m.Policy)
	}
	roster, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil || !strings.Contains(string(roster), "acme-owner") {
		t.Fatalf("managed roster was not refreshed: %v\n%s", err, roster)
	}

	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"agent", "list", "--json"}); code != 0 {
		t.Fatalf("list exit %d: %s", code, errOut.String())
	}
	var agents []manifest.Agent
	if err := json.Unmarshal(out.Bytes(), &agents); err != nil || len(agents) != 1 {
		t.Fatalf("list = %s, %v", out.String(), err)
	}

	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"agent", "remove", "acme-owner"}); code != 0 {
		t.Fatalf("remove exit %d: %s", code, errOut.String())
	}
	m, err = manifest.LoadDir(root)
	if err != nil || len(m.Agents) != 0 {
		t.Fatalf("agents after remove = %#v, %v", m.Agents, err)
	}
}

func TestAuthoringHelpersPreserveCommentsOrderAndStyles(t *testing.T) {
	root := t.TempDir()
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"init", "--example", "lib", "--id", "acme-lib"}); code != 0 {
		t.Fatalf("init exit %d: %s", code, errOut.String())
	}
	before, err := os.ReadFile(filepath.Join(root, "a2amodule.yml"))
	if err != nil {
		t.Fatal(err)
	}
	comments := []string{
		"# yaml-language-server: $schema=https://git-a2a.com/schema/a2amodule.v1.json",
		"# Describe the reusable code and the exact native import names consumers should wire.",
		"# Bind ownership and contact routes; the Agent Card remains the agent's description.",
		"# Route each request intent to a role and state the consumer boundary.",
	}
	for _, comment := range comments {
		if strings.Count(string(before), comment) != 1 {
			t.Fatalf("template does not contain comment %q exactly once:\n%s", comment, before)
		}
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"agent", "add", "acme-reviewer", "--role", "reviewer", "--scope", "docs/**"}); code != 0 {
		t.Fatalf("agent add exit %d: %s", code, errOut.String())
	}
	after, err := os.ReadFile(filepath.Join(root, "a2amodule.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, comment := range comments {
		if strings.Count(string(after), comment) != 1 {
			t.Errorf("comment changed or moved out of document: %q\n%s", comment, after)
		}
	}
	if !strings.Contains(string(after), `scope: ["**"]`) {
		t.Fatalf("original flow-style scope was rewritten:\n%s", after)
	}
	if strings.Index(string(after), "name: acme-lib-owner") > strings.Index(string(after), "name: acme-reviewer") {
		t.Fatalf("new agent was not appended:\n%s", after)
	}
}

func TestAuthoringHelperFailureDoesNotChangeManifest(t *testing.T) {
	root := t.TempDir()
	writeAuthorManifest(t, root)
	path := filepath.Join(root, "a2amodule.yml")
	before, _ := os.ReadFile(path)
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"agent", "add", "bad", "--role", "owner", "--contact", "intents=question,kind=email,url=https://wrong.example"}); code != 2 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid authoring command changed manifest")
	}
}

func TestInitExamplesAreCompleteValidManifests(t *testing.T) {
	for _, kind := range []string{"lib", "app"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			var out, errOut bytes.Buffer
			app := New(&out, &errOut)
			app.Root = root
			if code := app.Run([]string{"init", "--example", kind, "--id", "acme-" + kind}); code != 0 {
				t.Fatalf("exit %d: %s", code, errOut.String())
			}
			body, err := os.ReadFile(filepath.Join(root, "a2amodule.yml"))
			if err != nil {
				t.Fatal(err)
			}
			m, err := manifest.Parse(body)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(body), "# yaml-language-server:") || len(m.Agents) == 0 || m.Policy == nil {
				t.Fatalf("incomplete example:\n%s", body)
			}
			known := map[string]bool{"read-surface": true, "ask": true, "open-issue": true, "propose-change": true, "commit": true, "edit-spec": true, "release": true}
			for _, token := range append(append([]string{}, m.Policy.Consumers.May...), m.Policy.Consumers.MayNot...) {
				if !known[token] {
					t.Errorf("example uses consumer token absent from the spec vocabulary: %s", token)
				}
			}
			if got := strings.Join(m.Policy.Consumers.May, ","); got != "read-surface,ask,open-issue,propose-change" {
				t.Errorf("may vocabulary = %s", got)
			}
			if got := strings.Join(m.Policy.Consumers.MayNot, ","); got != "commit,edit-spec,release" {
				t.Errorf("may-not vocabulary = %s", got)
			}
		})
	}
}

func writeAuthorManifest(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: acme-module\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
