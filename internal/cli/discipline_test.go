package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestValidatePrintsVerdictFirst(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a2amodule.yml")
	if err := os.WriteFile(path, []byte("schema: 2\nmodule: {id: INVALID}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"validate", path}); code != 1 {
		t.Fatalf("exit %d", code)
	}
	first := strings.Split(strings.TrimSpace(errOut.String()), "\n")[0]
	if first != "1 file(s): validation failed" {
		t.Fatalf("first stderr line = %q; all stderr:\n%s", first, errOut.String())
	}
}

func TestEveryCommandHasCommandSpecificHelp(t *testing.T) {
	commands := []string{"init", "validate", "add", "set", "pin", "unpin", "wire", "update", "remove", "fetch", "show", "sync", "who", "contact", "status", "card", "catalog", "agent", "export", "policy", "explain", "fmt", "doctor", "usage", "version", "upgrade"}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := New(&out, &errOut).Run([]string{command, "--help"}); code != 0 {
				t.Fatalf("exit %d: %s", code, errOut.String())
			}
			if !strings.HasPrefix(out.String(), "usage: git-a2a "+command) {
				t.Fatalf("help = %q", out.String())
			}
		})
	}
}

func TestCLIReferenceTracksCommandHelp(t *testing.T) {
	document, err := os.ReadFile(filepath.Join("..", "..", "docs", "cli.md"))
	if err != nil {
		t.Fatal(err)
	}
	commands := []string{"init", "validate", "add", "set", "pin", "unpin", "wire", "update", "remove", "fetch", "show", "sync", "who", "contact", "status", "card", "catalog", "agent", "export", "policy", "explain", "fmt", "doctor", "usage", "version", "upgrade"}
	flagPattern := regexp.MustCompile(`--[a-z][a-z-]*`)
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			heading := "## " + command + "\n"
			start := strings.Index(string(document), heading)
			if start < 0 {
				t.Fatalf("docs/cli.md has no %q section", command)
			}
			section := string(document[start+len(heading):])
			if end := strings.Index(section, "\n## "); end >= 0 {
				section = section[:end]
			}
			var out, errOut bytes.Buffer
			if code := New(&out, &errOut).Run([]string{command, "--help"}); code != 0 {
				t.Fatalf("help exit %d: %s", code, errOut.String())
			}
			usageLine := strings.SplitN(out.String(), "\n", 2)[0]
			for _, flag := range flagPattern.FindAllString(usageLine, -1) {
				if !strings.Contains(section, flag) {
					t.Errorf("%s help flag %s is absent from docs section", command, flag)
				}
			}
			if !strings.Contains(section, "Exit") && !strings.Contains(section, "exit") {
				t.Error("section does not state an exit-code outcome")
			}
			if !strings.Contains(section, "```text") {
				t.Error("section has no example output")
			}
		})
	}
}

func TestInitAcceptsYesAsNoOp(t *testing.T) {
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	root := t.TempDir()
	app.Root = root
	if code := app.Run([]string{"init", "--yes", "--id", "demo"}); code != 0 {
		t.Fatalf("exit/output = %d, %q", code, errOut.String())
	}
	m, err := os.ReadFile(filepath.Join(root, "a2amodule.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(m), "id: demo") {
		t.Fatalf("manifest = %q", m)
	}
	errOut.Reset()
	if code := app.Run([]string{"fmt", "--check"}); code != 0 {
		t.Fatalf("fresh manifest is not canonical: exit/output = %d, %q", code, errOut.String())
	}
}

func TestFmtAcceptsFileAndDirectoryPaths(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "nested")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "one.yml"), filepath.Join(dir, "a2amodule.yml")} {
		if err := os.WriteFile(path, []byte("schema: 1\nmodule:\n    id: demo\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"fmt", "one.yml", "nested"}); code != 0 {
		t.Fatalf("exit/output = %d, %q", code, errOut.String())
	}
	for _, path := range []string{filepath.Join(root, "one.yml"), filepath.Join(dir, "a2amodule.yml")} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(got), "    id:") {
			t.Fatalf("%s was not formatted:\n%s", path, got)
		}
	}
	errOut.Reset()
	if code := app.Run([]string{"fmt", "--check", "one.yml", "nested"}); code != 0 {
		t.Fatalf("check exit/output = %d, %q", code, errOut.String())
	}
}

func TestPinRejectsShortSHAWithActionableMessage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := New(&out, &errOut).Run([]string{"pin", "demo", "deadbeef"}); code != 2 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "full 40-character SHA") {
		t.Fatalf("message = %q", errOut.String())
	}
}
