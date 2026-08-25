package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestContactA2AFromStdinPrintsOneRecordAndStoresNothing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"jsonrpc":"2.0","result":{"task":{"id":"task-42","status":{"state":"TASK_STATE_SUBMITTED"}}}}`)
	}))
	defer server.Close()
	root := t.TempDir()
	cache := filepath.Join(root, ".git-a2a", "cache", "acme-lib")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "schema: 1\nmodule: {id: acme-lib}\nagents:\n  - name: owner\n    role: owner\n    contacts:\n      - intents: [change]\n        kind: a2a\n        url: " + server.URL + "\n"
	if err := os.WriteFile(filepath.Join(cache, "a2amodule.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	before := directoryFiles(t, root)
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.In = strings.NewReader("please change it")
	app.Root = root
	if code := app.Run([]string{"contact", "acme-lib", "--intent", "change", "--message", "-"}); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], `agent="owner" kind=a2a id="task-42" state=TASK_STATE_SUBMITTED`) {
		t.Fatalf("stdout=%q", out.String())
	}
	after := directoryFiles(t, root)
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("contact stored state: before=%v after=%v", before, after)
	}
}

func TestContactPreservesOrderForInstructionDriver(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, ".git-a2a", "cache", "acme-lib")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "schema: 1\nmodule: {id: acme-lib}\nagents:\n  - name: owner\n    role: owner\n    contacts:\n      - intents: [question]\n        kind: email\n        address: owner@example.test\n      - intents: [question]\n        kind: a2a\n        url: https://should-not-run.example.test\n"
	if err := os.WriteFile(filepath.Join(cache, "a2amodule.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "message.md"), []byte("Question\nDetails\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"ask", "acme-lib", "--message", "message.md", "--dry-run"}); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut.String())
	}
	if strings.Count(strings.TrimSpace(out.String()), "\n") != 0 || !strings.Contains(out.String(), "kind=email") || !strings.Contains(out.String(), "state=instruction") {
		t.Fatalf("stdout=%q", out.String())
	}
}

func TestContactRefusesExternalConsumerUnlessExplicitlyApproved(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, ".git-a2a", "cache", "acme-lib")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	owner := `schema: 1
module:
  id: acme-lib
  repository: https://github.com/acme/lib
agents:
  - name: owner
    role: owner
    trust: {accepts-external: false}
    contacts:
      - intents: [question]
        kind: email
        address: owner@example.test
`
	consumer := "schema: 1\nmodule:\n  id: consumer-app\n  repository: https://github.com/consumer/app\n"
	if err := os.WriteFile(filepath.Join(cache, "a2amodule.yml"), []byte(owner), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a2amodule.yml"), []byte(consumer), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	app.In = strings.NewReader("question")
	if code := app.Run([]string{"contact", "acme-lib", "--message", "-"}); code != 2 || !strings.Contains(errOut.String(), "owner does not accept external requests") {
		t.Fatalf("exit=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	app.In = strings.NewReader("question")
	if code := app.Run([]string{"contact", "acme-lib", "--message", "-", "--external-ok", "--dry-run"}); code != 0 || !strings.Contains(out.String(), "kind=email") || !strings.Contains(errOut.String(), "override recorded") {
		t.Fatalf("override exit=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}

func TestContactListDriversNeedsNoMessage(t *testing.T) {
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	if code := app.Run([]string{"contact", "--list-drivers"}); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut.String())
	}
	for _, kind := range []string{"kind=github-issue", "kind=gitlab-issue", "kind=gitea-issue", "kind=http", "kind=exec"} {
		if !strings.Contains(out.String(), kind) {
			t.Errorf("missing %s:\n%s", kind, out.String())
		}
	}
}

func TestContactMissingArgumentsNamesFileAndStdinClearly(t *testing.T) {
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	if code := app.Run([]string{"contact"}); code != 2 {
		t.Fatalf("exit=%d stderr=%q", code, errOut.String())
	}
	want := "contact: module id and --message <file> (or \"-\" for stdin) are required\n"
	if got := errOut.String(); got != want {
		t.Fatalf("stderr=%q, want %q", got, want)
	}
}

func TestContactMCPRefusesDeclaredExec(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, ".git-a2a", "cache", "acme-lib")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	owner := "schema: 1\nmodule: {id: acme-lib}\nagents:\n  - name: owner\n    role: owner\n    contacts:\n      - intents: [change]\n        kind: exec\n        command: [acme-tracker]\n"
	consumer := "schema: 1\nmodule: {id: consumer-app}\nsettings:\n  contact:\n    allow-exec: [acme-tracker]\n"
	if err := os.WriteFile(filepath.Join(cache, "a2amodule.yml"), []byte(owner), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a2amodule.yml"), []byte(consumer), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	app.In = strings.NewReader("change")
	app.mcpInvocation = true
	if code := app.Run([]string{"contact", "acme-lib", "--intent", "change", "--message", "-"}); code != 1 || !strings.Contains(errOut.String(), "refused through MCP") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func TestContactPluginPrecedesInstruction(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX plugin fixture; .cmd is covered in plugin package")
	}
	root := t.TempDir()
	cache := filepath.Join(root, ".git-a2a", "cache", "acme-lib")
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	owner := "schema: 1\nmodule: {id: acme-lib}\nagents:\n  - name: owner\n    role: owner\n    contacts:\n      - intents: [change]\n        kind: acme-tracker\n        queue: platform\n"
	if err := os.WriteFile(filepath.Join(cache, "a2amodule.yml"), []byte(owner), 0o644); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(bin, "git-a2a-contact-acme-tracker")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\ncat >/dev/null\necho '{\"id\":\"ACME-9\",\"state\":\"created\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	app.In = strings.NewReader("change")
	if code := app.Run([]string{"contact", "acme-lib", "--intent", "change", "--message", "-"}); code != 0 || !strings.Contains(out.String(), "driver=plugin:git-a2a-contact-acme-tracker") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func directoryFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			relative, _ := filepath.Rel(root, path)
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			files = append(files, relative+"\x00"+string(body))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return files
}
