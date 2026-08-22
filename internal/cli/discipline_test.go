package cli

import (
	"bytes"
	"os"
	"path/filepath"
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
	commands := []string{"init", "validate", "add", "set", "pin", "unpin", "wire", "update", "remove", "show", "sync", "who", "status", "card", "fmt", "version", "upgrade"}
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
