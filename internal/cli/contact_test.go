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
