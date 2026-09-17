// Package cardmetadata prepares and transactionally materializes repository-
// relative Agent Cards. It deliberately treats cards as opaque Git blobs: the
// bytes are neither parsed nor rewritten.
package cardmetadata

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/neprel/git-a2a/v2/internal/manifest"
)

const fileName = "agent-card.json"

var aliasPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?$`)

// FileFetcher reads one byte-exact file from a resolved Git commit.
type FileFetcher interface {
	File(ctx context.Context, source, commit, filePath string) ([]byte, error)
}

// Prepared is read-only preparation for a later transactional Apply.
type Prepared struct {
	Agent manifest.LockedAgent
	bytes []byte
}

// Prepare resolves an upstream card declaration. HTTPS references remain
// external; repository-relative declarations are fetched relative to the
// upstream manifest root from the already resolved commit.
func Prepare(ctx context.Context, fetcher FileFetcher, alias, source, commit, manifestRoot string, agent manifest.Agent) (Prepared, error) {
	if !aliasPattern.MatchString(alias) {
		return Prepared{}, fmt.Errorf("agent card alias %q is invalid", alias)
	}
	if !isCommit(commit) {
		return Prepared{}, fmt.Errorf("agent card commit must be a 40-character lowercase Git object ID")
	}
	locked := manifest.LockedAgent{Name: agent.Name, DeclaredCard: agent.Card, Card: agent.Card, Commit: commit}
	if isHTTPS(agent.Card) {
		return Prepared{Agent: locked}, nil
	}
	if err := safeRelative("agent.card", agent.Card, false); err != nil {
		return Prepared{}, err
	}
	if manifestRoot == "" {
		manifestRoot = "."
	}
	if err := safeRelative("dependency.path", manifestRoot, true); err != nil {
		return Prepared{}, err
	}
	gitPath := path.Clean(path.Join(filepath.ToSlash(manifestRoot), filepath.ToSlash(agent.Card)))
	if gitPath == "." || gitPath == ".." || strings.HasPrefix(gitPath, "../") {
		return Prepared{}, fmt.Errorf("agent.card resolves outside the upstream manifest root")
	}
	body, err := fetcher.File(ctx, source, commit, gitPath)
	if err != nil {
		return Prepared{}, fmt.Errorf("fetch agent card %s at %s: %w", gitPath, commit, err)
	}
	locked.Card = ResolvedPath(alias)
	return Prepared{Agent: locked, bytes: body}, nil
}

// ResolvedPath returns the stable consumer-relative reference used in locks
// and list output for a repository-relative card.
func ResolvedPath(alias string) string {
	return path.Join(".git-a2a", "agents", alias, fileName)
}

// Apply replaces the alias metadata atomically. For an HTTPS card this removes
// an obsolete local snapshot. The returned restore and finalize callbacks let
// lifecycle include the operation in its wider dependency transaction.
func Apply(root, alias string, prepared Prepared) (restore func() error, finalize func(), err error) {
	if !aliasPattern.MatchString(alias) {
		return nil, nil, fmt.Errorf("agent card alias %q is invalid", alias)
	}
	parent := filepath.Join(root, ".git-a2a", "agents")
	if err := ensureSafeDirectories(root, ".git-a2a", "agents"); err != nil {
		return nil, nil, err
	}
	target := filepath.Join(parent, alias)
	if info, statErr := os.Lstat(target); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("agent metadata path %s must not be a symlink", target)
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return nil, nil, statErr
	}

	var stage string
	if !isHTTPS(prepared.Agent.DeclaredCard) {
		if prepared.Agent.Card != ResolvedPath(alias) {
			return nil, nil, fmt.Errorf("agent card resolved path %q does not match alias %q", prepared.Agent.Card, alias)
		}
		stage, err = os.MkdirTemp(parent, "."+alias+"-card-")
		if err != nil {
			return nil, nil, err
		}
		if err = os.WriteFile(filepath.Join(stage, fileName), prepared.bytes, 0o644); err != nil {
			_ = os.RemoveAll(stage)
			return nil, nil, err
		}
	}

	backup, err := reservePath(parent, "."+alias+"-previous-")
	if err != nil {
		_ = os.RemoveAll(stage)
		return nil, nil, err
	}
	hadPrevious := false
	if _, statErr := os.Lstat(target); statErr == nil {
		if err = os.Rename(target, backup); err != nil {
			_ = os.RemoveAll(stage)
			return nil, nil, err
		}
		hadPrevious = true
	} else if !os.IsNotExist(statErr) {
		_ = os.RemoveAll(stage)
		return nil, nil, statErr
	}
	if stage != "" {
		if err = os.Rename(stage, target); err != nil {
			if hadPrevious {
				_ = os.Rename(backup, target)
			}
			return nil, nil, err
		}
	}
	restore = func() error {
		var failures []error
		if removeErr := os.RemoveAll(target); removeErr != nil {
			failures = append(failures, removeErr)
		}
		if hadPrevious {
			if renameErr := os.Rename(backup, target); renameErr != nil {
				failures = append(failures, renameErr)
			}
		}
		return errors.Join(failures...)
	}
	finalize = func() { _ = os.RemoveAll(backup) }
	return restore, finalize, nil
}

// Remove transactionally removes metadata for an alias.
func Remove(root, alias string) (restore func() error, finalize func(), err error) {
	if !aliasPattern.MatchString(alias) {
		return nil, nil, fmt.Errorf("agent card alias %q is invalid", alias)
	}
	target := filepath.Join(root, ".git-a2a", "agents", alias)
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return func() error { return nil }, func() {}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("agent metadata path %s must not be a symlink", target)
	}
	backup, err := reservePath(filepath.Dir(target), "."+alias+"-removed-")
	if err != nil {
		return nil, nil, err
	}
	if err = os.Rename(target, backup); err != nil {
		return nil, nil, err
	}
	return func() error { return os.Rename(backup, target) }, func() { _ = os.RemoveAll(backup) }, nil
}

// ForList returns a copy suitable for offline list output. A missing or unsafe
// local file is never returned as a usable card reference.
func ForList(root, alias string, locked manifest.LockedAgent) (manifest.LockedAgent, []string) {
	if isHTTPS(locked.DeclaredCard) {
		return locked, nil
	}
	want := ResolvedPath(alias)
	if locked.Card != want {
		locked.Card = ""
		return locked, []string{fmt.Sprintf("agent card has invalid resolved path; run git a2a pull %s", alias)}
	}
	full := filepath.Join(root, filepath.FromSlash(want))
	if err := ensureExistingPathHasNoSymlink(root, filepath.FromSlash(want)); err != nil {
		locked.Card = ""
		return locked, []string{fmt.Sprintf("agent card unavailable: %v; run git a2a pull %s", err, alias)}
	}
	info, err := os.Stat(full)
	if err != nil {
		locked.Card = ""
		return locked, []string{fmt.Sprintf("agent card unavailable: %v; run git a2a pull %s", err, alias)}
	}
	if !info.Mode().IsRegular() {
		locked.Card = ""
		return locked, []string{fmt.Sprintf("agent card is not a regular file; run git a2a pull %s", alias)}
	}
	return locked, nil
}

func reservePath(parent, pattern string) (string, error) {
	p, err := os.MkdirTemp(parent, pattern)
	if err != nil {
		return "", err
	}
	if err = os.Remove(p); err != nil {
		return "", err
	}
	return p, nil
}

func ensureSafeDirectories(root string, elements ...string) error {
	current := root
	for _, element := range elements {
		current = filepath.Join(current, element)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err = os.Mkdir(current, 0o755); err != nil && !os.IsExist(err) {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("agent metadata parent %s must be a directory, not a symlink", current)
		}
	}
	return nil
}

func ensureExistingPathHasNoSymlink(root, relative string) error {
	current := root
	for _, element := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, element)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink", current)
		}
	}
	return nil
}

func safeRelative(name, value string, allowDot bool) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	clean := filepath.ToSlash(filepath.Clean(value))
	if clean == "." && allowDot {
		return nil
	}
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || filepath.IsAbs(value) || strings.Contains(value, "\\") {
		return fmt.Errorf("%s must be a safe repository-relative path", name)
	}
	return nil
}

func isHTTPS(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Host != ""
}

func isCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}
