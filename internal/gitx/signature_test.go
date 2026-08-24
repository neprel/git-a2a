package gitx

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runCommand(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v: %s", name, args, err, out)
	}
	return string(out)
}

func runGitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return runCommand(t, dir, "git", args...)
}

func TestVerifySignedObjectWithSSHAllowedSigners(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen is unavailable")
	}
	root := t.TempDir()
	key := filepath.Join(root, "demo_signing_key")
	runCommand(t, root, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key)
	public, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(root, "allowed_signers")
	if err := os.WriteFile(allowed, []byte("acme@example.test "+strings.TrimSpace(string(public))+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repo")
	runGitTest(t, "", "init", repo)
	runGitTest(t, repo, "config", "user.name", "Acme Demo")
	runGitTest(t, repo, "config", "user.email", "acme@example.test")
	runGitTest(t, repo, "config", "gpg.format", "ssh")
	runGitTest(t, repo, "config", "user.signingKey", key)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repo, "add", "README.md")
	runGitTest(t, repo, "commit", "-S", "-m", "signed")
	commit := strings.TrimSpace(runGitTest(t, repo, "rev-parse", "HEAD"))
	if err := VerifySignedObject(context.Background(), ExecRunner{}, repo, commit, "refs/heads/main", "branch", allowed, filepath.Join(root, "work")); err != nil {
		t.Fatal(err)
	}
	otherKey := filepath.Join(root, "other_signing_key")
	runCommand(t, root, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", otherKey)
	otherPublic, err := os.ReadFile(otherKey + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	wrongAllowed := filepath.Join(root, "wrong_allowed_signers")
	if err := os.WriteFile(wrongAllowed, []byte("other@example.test "+strings.TrimSpace(string(otherPublic))+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifySignedObject(context.Background(), ExecRunner{}, repo, commit, "refs/heads/main", "branch", wrongAllowed, filepath.Join(root, "wrong-work")); err == nil {
		t.Fatal("commit signed by a disallowed principal unexpectedly verified")
	} else {
		fingerprint := strings.Fields(runCommand(t, root, "ssh-keygen", "-lf", key+".pub", "-E", "sha256"))[1]
		want := fmt.Sprintf("commit %.7s signature rejected: untrusted key %s (signer none, fingerprint %s); allowed signers: %s",
			commit, fingerprint, fingerprint, wrongAllowed)
		if err.Error() != want {
			t.Fatalf("rejected signature error\n got: %q\nwant: %q", err, want)
		}
	}

	unsigned := filepath.Join(root, "unsigned")
	runGitTest(t, "", "clone", repo, unsigned)
	runGitTest(t, unsigned, "config", "user.name", "Acme Demo")
	runGitTest(t, unsigned, "config", "user.email", "acme@example.test")
	if err := os.WriteFile(filepath.Join(unsigned, "README.md"), []byte("unsigned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, unsigned, "add", "README.md")
	runGitTest(t, unsigned, "commit", "--no-gpg-sign", "-m", "unsigned")
	unsignedCommit := strings.TrimSpace(runGitTest(t, unsigned, "rev-parse", "HEAD"))
	if err := VerifySignedObject(context.Background(), ExecRunner{}, unsigned, unsignedCommit, "refs/heads/main", "branch", allowed, filepath.Join(root, "bad-work")); err == nil {
		t.Fatal("unsigned commit unexpectedly verified")
	} else {
		want := fmt.Sprintf("commit %.7s is not signed; allowed signers: %s", unsignedCommit, allowed)
		if err.Error() != want {
			t.Fatalf("unsigned signature error\n got: %q\nwant: %q", err, want)
		}
	}
}
