package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	if code := app.Run([]string{"ask", "acme-lib", "--message", "message.md"}); code != 0 {
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
	if code := app.Run([]string{"contact", "acme-lib", "--message", "-", "--external-ok"}); code != 0 || !strings.Contains(out.String(), "kind=email") || !strings.Contains(errOut.String(), "override recorded") {
		t.Fatalf("override exit=%d out=%q err=%q", code, out.String(), errOut.String())
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
