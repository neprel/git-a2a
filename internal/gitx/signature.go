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
	if _, err := runner.Run(ctx, repo, nil,
		"-c", "gpg.ssh.allowedSignersFile="+allowedSigners,
		verifyCommand, "--raw", verifyTarget); err != nil {
		return fmt.Errorf("commit signature: %w", err)
	}
	return nil
}
