package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/cli"
	"github.com/neprel/git-a2a/internal/manifest"
)

func TestAddUpdateCheckRemoveAgainstLocalBareRepository(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	bare := filepath.Join(tmp, "library.git")
	consumer := filepath.Join(tmp, "consumer")
	mustMkdir(t, source)
	git(t, source, "init", "-b", "main")
	git(t, source, "config", "user.email", "test@example.com")
	git(t, source, "config", "user.name", "Test")
	manifestBytes := []byte("schema: 1\nmodule:\n  id: acme-lib-utils\n  release:\n    channel: main\n")
	mustWrite(t, filepath.Join(source, "a2amodule.yml"), manifestBytes)
	git(t, source, "add", "a2amodule.yml")
	git(t, source, "commit", "-m", "fixture")
	git(t, tmp, "clone", "--bare", source, bare)
	git(t, bare, "config", "uploadpack.allowFilter", "true")
	git(t, bare, "config", "uploadpack.allowAnySHA1InWant", "true")
	mustMkdir(t, consumer)
	mustWrite(t, filepath.Join(consumer, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: acme-app-cli\n"))
	var out, errOut bytes.Buffer
	app := cli.New(&out, &errOut)
	app.Root = consumer
	url := "file://" + bare
	if code := app.Run([]string{"add", url}); code != 0 {
		t.Fatalf("add exit %d: %s", code, errOut.String())
	}
	if !strings.HasPrefix(errOut.String(), "using declared release channel main\n") {
		t.Fatalf("unexpected stderr: %q", errOut.String())
	}
	lock, err := manifest.LoadLock(filepath.Join(consumer, "a2amodule.lock"))
	if err != nil {
		t.Fatal(err)
	}
	old := lock.Dependencies["acme-lib-utils"].Commit
	if len(old) != 40 {
		t.Fatalf("bad commit %q", old)
	}
	if _, err := os.Stat(filepath.Join(consumer, ".git-a2a", "cache", "acme-lib-utils", "a2amodule.yml")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"update", "--check"}); code != 0 {
		t.Fatalf("clean check exit %d: %s", code, errOut.String())
	}
	mustWrite(t, filepath.Join(source, "a2amodule.yml"), append(manifestBytes, []byte("x-revision: two\n")...))
	git(t, source, "add", "a2amodule.yml")
	git(t, source, "commit", "-m", "update")
	git(t, source, "push", bare, "main")
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"update", "--check"}); code != 1 {
		t.Fatalf("changed check exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "acme-lib-utils:") {
		t.Fatalf("change did not name dependency: %q", out.String())
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"update"}); code != 0 {
		t.Fatalf("update exit %d: %s", code, errOut.String())
	}
	next, _ := manifest.LoadLock(filepath.Join(consumer, "a2amodule.lock"))
	if next.Dependencies["acme-lib-utils"].Commit == old {
		t.Fatal("lock did not advance")
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"remove", "acme-lib-utils"}); code != 0 {
		t.Fatalf("remove exit %d: %s", code, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(consumer, ".git-a2a", "cache", "acme-lib-utils")); !os.IsNotExist(err) {
		t.Fatalf("cache still exists: %v", err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}
func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}
func mustWrite(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
