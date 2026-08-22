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
	if !strings.HasPrefix(errOut.String(), "added acme-lib-utils at ") || !strings.Contains(errOut.String(), "using declared release channel main\n") {
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

func TestSetPinUnpinSourceAndMovedAnnouncement(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	original := filepath.Join(tmp, "original.git")
	fork := filepath.Join(tmp, "fork.git")
	other := filepath.Join(tmp, "other.git")
	consumer := filepath.Join(tmp, "consumer")
	mustMkdir(t, source)
	git(t, source, "init", "-b", "main")
	git(t, source, "config", "user.email", "test@example.com")
	git(t, source, "config", "user.name", "Test")
	base := []byte("schema: 1\nmodule:\n  id: acme-lib-utils\n  repository: file:///canonical.git\n  release:\n    channel: main\n")
	mustWrite(t, filepath.Join(source, "a2amodule.yml"), base)
	git(t, source, "add", "a2amodule.yml")
	git(t, source, "commit", "-m", "one")
	tagCommit := strings.TrimSpace(gitOutput(t, source, "rev-parse", "HEAD"))
	git(t, source, "tag", "release")
	mustWrite(t, filepath.Join(source, "a2amodule.yml"), append(base, []byte("x-revision: two\n")...))
	git(t, source, "add", "a2amodule.yml")
	git(t, source, "commit", "-m", "two")
	git(t, source, "branch", "release")
	git(t, tmp, "clone", "--bare", source, original)
	git(t, tmp, "clone", "--bare", source, fork)
	mustMkdir(t, consumer)
	mustWrite(t, filepath.Join(consumer, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: acme-app-cli\n"))
	var out, errOut bytes.Buffer
	app := cli.New(&out, &errOut)
	app.Root = consumer
	if code := app.Run([]string{"add", "file://" + original, "--no-wire"}); code != 0 {
		t.Fatalf("add %d: %s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"set", "acme-lib-utils", "--git", "file://" + fork}); code != 0 {
		t.Fatalf("set git %d: %s", code, errOut.String())
	}
	m, err := manifest.Load(filepath.Join(consumer, "a2amodule.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Dependencies[0].Git != "file://"+fork {
		t.Fatal("source did not switch")
	}
	out.Reset()
	errOut.Reset()
	_ = app.Run([]string{"status", "acme-lib-utils", "--offline"})
	if !strings.Contains(out.String(), "fork of file:///canonical.git") || !strings.Contains(out.String(), "branch main") {
		t.Fatalf("fork status missing source/ref: %s", out.String())
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"set", "acme-lib-utils", "--ref", "release"}); code != 0 {
		t.Fatalf("set ref %d: %s", code, errOut.String())
	}
	l, _ := manifest.LoadLock(filepath.Join(consumer, "a2amodule.lock"))
	if l.Dependencies["acme-lib-utils"].Commit != tagCommit {
		t.Fatalf("ambiguous ref did not choose tag: %s want %s", l.Dependencies["acme-lib-utils"].Commit, tagCommit)
	}
	if !strings.Contains(errOut.String(), "selected refs/tags/release") {
		t.Fatalf("ambiguity not reported: %s", errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"pin", "acme-lib-utils"}); code != 0 {
		t.Fatalf("pin %d: %s", code, errOut.String())
	}
	m, _ = manifest.Load(filepath.Join(consumer, "a2amodule.yml"))
	if len(m.Dependencies[0].Ref) != 40 || m.Dependencies[0].Track != "locked" {
		t.Fatalf("not pinned: %#v", m.Dependencies[0])
	}
	out.Reset()
	errOut.Reset()
	_ = app.Run([]string{"status", "acme-lib-utils", "--offline"})
	if !strings.Contains(out.String(), "pinned "+tagCommit[:12]) {
		t.Fatalf("pinned status missing ref: %s", out.String())
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"unpin", "acme-lib-utils", "--ref", "main"}); code != 0 {
		t.Fatalf("unpin %d: %s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"unpin", "acme-lib-utils", "--ref", "main", "--track", "invalid"}); code != 2 {
		t.Fatalf("invalid unpin track exit %d: %s", code, errOut.String())
	}
	otherSource := filepath.Join(tmp, "other-source")
	mustMkdir(t, otherSource)
	git(t, otherSource, "init", "-b", "main")
	git(t, otherSource, "config", "user.email", "test@example.com")
	git(t, otherSource, "config", "user.name", "Test")
	mustWrite(t, filepath.Join(otherSource, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: different-module\n"))
	git(t, otherSource, "add", "a2amodule.yml")
	git(t, otherSource, "commit", "-m", "other")
	git(t, tmp, "clone", "--bare", otherSource, other)
	beforeManifest, _ := os.ReadFile(filepath.Join(consumer, "a2amodule.yml"))
	beforeLock, _ := os.ReadFile(filepath.Join(consumer, "a2amodule.lock"))
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"set", "acme-lib-utils", "--git", "file://" + other}); code != 1 {
		t.Fatalf("id mismatch exit %d", code)
	}
	afterManifest, _ := os.ReadFile(filepath.Join(consumer, "a2amodule.yml"))
	afterLock, _ := os.ReadFile(filepath.Join(consumer, "a2amodule.lock"))
	if !bytes.Equal(beforeManifest, afterManifest) || !bytes.Equal(beforeLock, afterLock) {
		t.Fatal("id mismatch changed metadata")
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"set", "acme-lib-utils", "--git", "file://" + original, "--ref", "main"}); code != 0 {
		t.Fatalf("back to original %d: %s", code, errOut.String())
	}
	moved := append(base, []byte("  moved-to:\n    git: file://"+fork+"\n")...)
	mustWrite(t, filepath.Join(source, "a2amodule.yml"), moved)
	git(t, source, "add", "a2amodule.yml")
	git(t, source, "commit", "-m", "moved")
	git(t, source, "push", original, "main")
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"update"}); code != 1 || !strings.Contains(errOut.String(), "moved to") {
		t.Fatalf("move not detected %d: %s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	_ = app.Run([]string{"status", "acme-lib-utils"})
	if !strings.Contains(out.String(), "moved → file://"+fork) {
		t.Fatalf("moved status missing source: %s", out.String())
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"update", "--follow-moves"}); code != 0 {
		t.Fatalf("follow move %d: %s", code, errOut.String())
	}
	m, _ = manifest.Load(filepath.Join(consumer, "a2amodule.yml"))
	if m.Dependencies[0].Git != "file://"+fork {
		t.Fatalf("move not followed: %#v", m.Dependencies[0])
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
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
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
