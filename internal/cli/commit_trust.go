package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/neprel/git-a2a/internal/fetch"
	"github.com/neprel/git-a2a/internal/gitx"
	"github.com/neprel/git-a2a/internal/manifest"
)

func (a *App) verifyCommitTrust(root string, dependency manifest.Dependency, result fetch.Result, kind string, insecureSkip bool, work string) (string, error) {
	if dependency.Require == nil || dependency.Require.Commits != "signed" {
		return "", nil
	}
	if insecureSkip {
		return "skipped", nil
	}
	signers, err := repositoryRelativeFile(root, dependency.Require.Signers)
	if err != nil {
		return "", fmt.Errorf("%s: %w", dependency.ID, err)
	}
	if kind == "" {
		if strings.HasPrefix(result.Ref, "refs/tags/") {
			kind = "tag"
		} else {
			kind = "commit"
		}
	}
	if err := gitx.VerifySignedObject(a.context(), a.runner(), dependency.Git, result.Commit, result.Ref, kind, signers, filepath.Join(work, "signature")); err != nil {
		return "", fmt.Errorf("%s: %w", dependency.ID, err)
	}
	return "signed", nil
}

func repositoryRelativeFile(root, relative string) (string, error) {
	if relative == "" {
		return "", fmt.Errorf("require.signers is required when require.commits is signed")
	}
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("require.signers must be repository-relative")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(root, filepath.Clean(relative)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("require.signers must stay inside the repository")
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", err
	}
	resolvedRel, err := filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("require.signers must stay inside the repository")
	}
	return target, nil
}
