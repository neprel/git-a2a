package gitx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// VerifySignedObject fetches only the resolved object into a scratch repository and asks Git
// to verify it. Git remains the authority for SSH and OpenPGP signature formats.
func VerifySignedObject(ctx context.Context, runner Runner, url, commit, fullRef, kind, allowedSigners, work string) error {
	if runner == nil {
		return fmt.Errorf("commit signature: git runner is required")
	}
	if allowedSigners == "" {
		return fmt.Errorf("commit signature: allowed signers file is required")
	}
	if _, err := os.Stat(allowedSigners); err != nil {
		return fmt.Errorf("commit signature: allowed signers file %s: %w", allowedSigners, err)
	}
	repo := filepath.Join(work, "signature-repository")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		return fmt.Errorf("commit signature: %w", err)
	}
	if _, err := runner.Run(ctx, repo, nil, "init", "--quiet"); err != nil {
		return fmt.Errorf("commit signature: %w", err)
	}
	if _, err := runner.Run(ctx, repo, nil, "remote", "add", "origin", url); err != nil {
		return fmt.Errorf("commit signature: %w", err)
	}
	verifyTarget := commit
	verifyCommand := "verify-commit"
	if kind == "tag" && strings.HasPrefix(fullRef, "refs/tags/") {
		if _, err := runner.Run(ctx, repo, nil, "fetch", "--depth=1", "origin", fullRef+":"+fullRef); err != nil {
			return fmt.Errorf("commit signature: fetch tag: %w", err)
		}
		verifyTarget = fullRef
		verifyCommand = "verify-tag"
	} else if _, err := runner.Run(ctx, repo, nil, "fetch", "--depth=1", "origin", commit); err != nil {
		return fmt.Errorf("commit signature: fetch commit: %w", err)
	}
	_, verifyErr := runner.Run(ctx, repo, nil,
		"-c", "gpg.ssh.allowedSignersFile="+allowedSigners,
		verifyCommand, "--raw", verifyTarget)
	status, signer, key, fingerprint := signatureIdentity(ctx, runner, repo, allowedSigners, verifyTarget, verifyCommand)
	if verifyCommand == "verify-commit" && status == "unsigned" {
		return fmt.Errorf("commit %s is not signed; allowed signers: %s", shortCommit(commit), allowedSigners)
	}
	if verifyErr != nil || (verifyCommand == "verify-commit" && status != "good") {
		return fmt.Errorf("commit %s signature rejected: %s; allowed signers: %s",
			shortCommit(commit), signatureReason(status, signer, key, fingerprint), allowedSigners)
	}
	return nil
}

func signatureIdentity(ctx context.Context, runner Runner, repo, allowedSigners, target, command string) (string, string, string, string) {
	if command != "verify-commit" {
		return "rejected", "unknown", "unknown", "unknown"
	}
	out, err := runner.Run(ctx, repo, nil,
		"-c", "gpg.ssh.allowedSignersFile="+allowedSigners,
		"show", "-s", "--format=%G?%x00%GS%x00%GK%x00%GF", target)
	if err != nil {
		return "rejected", "unknown", "unknown", "unknown"
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\n"), "\x00")
	if len(fields) != 4 {
		return "rejected", "unknown", "unknown", "unknown"
	}
	status := map[string]string{
		"G": "good", "U": "untrusted", "N": "unsigned", "B": "bad", "E": "error",
		"X": "expired", "Y": "expired-key", "R": "revoked-key",
	}[fields[0]]
	if status == "" {
		status = "rejected"
	}
	return status, signatureField(fields[1]), signatureField(fields[2]), signatureField(fields[3])
}

func signatureField(value string) string {
	if value == "" {
		return "none"
	}
	return value
}

func signatureReason(status, signer, key, fingerprint string) string {
	label := map[string]string{
		"untrusted":   "untrusted key",
		"bad":         "bad signature from key",
		"error":       "signature verification error for key",
		"expired":     "expired signature from key",
		"expired-key": "expired key",
		"revoked-key": "revoked key",
		"good":        "verification failed for key",
		"rejected":    "rejected key",
	}[status]
	if label == "" {
		label = "rejected key"
	}
	return fmt.Sprintf("%s %s (signer %s, fingerprint %s)", label, key, signer, fingerprint)
}

func shortCommit(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}
