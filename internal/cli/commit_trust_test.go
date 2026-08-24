package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	lockfile "github.com/neprel/git-a2a/internal/lock"
)

func TestSignedCommitRequirementGuardsLockAndAllowsExplicitSkip(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen is unavailable")
	}
	base := t.TempDir()
	key := filepath.Join(base, "demo_signing_key")
	runExternal(t, base, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key)
	public, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(base, "source")
	remote := filepath.Join(base, "acme-lib.git")
	consumer := filepath.Join(base, "consumer")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	runFetchGit(t, source, "init", "-b", "main")
	runFetchGit(t, source, "config", "user.name", "Acme Demo")
	runFetchGit(t, source, "config", "user.email", "acme@example.test")
	runFetchGit(t, source, "config", "gpg.format", "ssh")
	runFetchGit(t, source, "config", "user.signingKey", key)
	manifestRaw := "schema: 1\nmodule:\n  id: acme-lib\n  repository: file://" + remote + "\n"
	if err := os.WriteFile(filepath.Join(source, "a2amodule.yml"), []byte(manifestRaw), 0o644); err != nil {
		t.Fatal(err)
	}
	runFetchGit(t, source, "add", "a2amodule.yml")
	runFetchGit(t, source, "commit", "-S", "-m", "signed module")
	runFetchGit(t, base, "clone", "--bare", source, remote)
	if err := os.MkdirAll(filepath.Join(consumer, "trust"), 0o755); err != nil {
		t.Fatal(err)
	}
	allowed := "acme@example.test " + strings.TrimSpace(string(public)) + "\n"
	if err := os.WriteFile(filepath.Join(consumer, "trust", "allowed_signers"), []byte(allowed), 0o644); err != nil {
		t.Fatal(err)
	}
	own := "schema: 1\nmodule:\n  id: consumer-app\ndependencies:\n  - id: acme-lib\n    git: file://" + remote + "\n    ref: main\n    require:\n      commits: signed\n      signers: trust/allowed_signers\n"
	if err := os.WriteFile(filepath.Join(consumer, "a2amodule.yml"), []byte(own), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = consumer
	if code := app.Run([]string{"add", "file://" + remote + "#main", "--no-refresh"}); code != 0 {
		t.Fatalf("signed add exit=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	locked, err := lockfile.Load(consumer)
	if err != nil {
		t.Fatal(err)
	}
	entry := locked.Dependencies["acme-lib"]
	if entry.Verified != "signed" {
		t.Fatalf("verified=%q", entry.Verified)
	}
	oldCommit := entry.Commit
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("unsigned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runFetchGit(t, source, "add", "README.md")
	runFetchGit(t, source, "commit", "--no-gpg-sign", "-m", "unsigned change")
	runFetchGit(t, source, "push", "file://"+remote, "main")
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"update", "acme-lib", "--no-refresh", "--no-review"}); code != 1 || !strings.Contains(errOut.String(), " is not signed; allowed signers: ") {
		t.Fatalf("unsigned update exit=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	locked, _ = lockfile.Load(consumer)
	if locked.Dependencies["acme-lib"].Commit != oldCommit {
		t.Fatal("failed verification changed lock")
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"update", "acme-lib", "--no-refresh", "--no-review", "--insecure-skip-signers"}); code != 0 {
		t.Fatalf("skip update exit=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	locked, _ = lockfile.Load(consumer)
	if locked.Dependencies["acme-lib"].Verified != "skipped" {
		t.Fatalf("skip verified=%q", locked.Dependencies["acme-lib"].Verified)
	}
}

func TestRepositoryRelativeSignersRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "allowed_signers")
	if err := os.WriteFile(outside, []byte("acme@example.test ssh-ed25519 AAAA\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "allowed_signers")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := repositoryRelativeFile(root, "allowed_signers"); err == nil || !strings.Contains(err.Error(), "must stay inside") {
		t.Fatalf("symlink escape error = %v", err)
	}
}

func runExternal(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v: %s", name, args, err, out)
	}
	return string(out)
}
