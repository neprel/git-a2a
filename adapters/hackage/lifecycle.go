package hackage

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/neprel/git-a2a/v2/internal/adapter"
	"github.com/neprel/git-a2a/v2/internal/gitx"
)

// Capability performs the source-shape portion of lifecycle preflight.
func (a Adapter) Capability(root string, _ adapter.Dependency, _ adapter.Export) error {
	ok, _, err := a.Detect(root)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("hackage: neither stack.yaml nor cabal.project was found")
	}
	return nil
}

// Pull converges both the native declaration and its local materialization.
func (a Adapter) Pull(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	if err := a.Capability(root, dep, exp); err != nil {
		return adapter.Change{}, err
	}
	_, variant, err := a.Detect(root)
	if err != nil {
		return adapter.Change{}, err
	}
	if err := adapter.RequireTool(ctx, "hackage", variant); err != nil {
		return adapter.Change{}, err
	}
	change, err := a.Wire(ctx, root, dep, exp, locked)
	if err != nil {
		return change, err
	}
	return change, hackageMaterialize(ctx, root, variant)
}

// Remove converges declaration, native lock state, and local materialization.
func (a Adapter) Remove(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	_, variant, err := a.Detect(root)
	if err != nil {
		return adapter.Change{}, err
	}
	if err := adapter.RequireTool(ctx, "hackage", variant); err != nil {
		return adapter.Change{}, err
	}
	change, err := a.Unwire(ctx, root, dep, exp)
	if err != nil {
		return change, err
	}
	return change, hackagePlan(ctx, root, variant)
}

// Inspect is local and read-only.
func (a Adapter) Inspect(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	findings, err := a.Drift(ctx, root, dep, exp, locked)
	if err != nil || len(findings) != 0 {
		return findings, err
	}
	_, variant, err := a.Detect(root)
	if err != nil {
		return nil, err
	}
	switch variant {
	case "stack":
		lockBody, readErr := os.ReadFile(filepath.Join(root, "stack.yaml.lock"))
		if os.IsNotExist(readErr) {
			return []adapter.Finding{{File: "stack.yaml.lock", Entry: exp.Name, Want: locked.Commit, Got: "missing", Repairable: true}}, nil
		}
		if readErr != nil {
			return nil, readErr
		}
		if !bytes.Contains(lockBody, []byte(locked.Commit)) {
			return []adapter.Finding{{File: "stack.yaml.lock", Entry: exp.Name, Want: locked.Commit, Got: "stale", Repairable: true}}, nil
		}
		if _, statErr := os.Stat(filepath.Join(root, ".stack-work")); os.IsNotExist(statErr) {
			return []adapter.Finding{{File: ".stack-work", Entry: exp.Name, Want: locked.Commit, Got: "missing", Repairable: true}}, nil
		} else if statErr != nil {
			return nil, statErr
		}
	case "cabal":
		planPath := filepath.Join(root, "dist-newstyle", "cache", "plan.json")
		planBody, readErr := os.ReadFile(planPath)
		if os.IsNotExist(readErr) {
			return []adapter.Finding{{File: "dist-newstyle/cache/plan.json", Entry: exp.Name, Want: locked.Commit, Got: "missing", Repairable: true}}, nil
		} else if readErr != nil {
			return nil, readErr
		}
		if !bytes.Contains(planBody, []byte(locked.Commit)) {
			return []adapter.Finding{{File: "dist-newstyle/cache/plan.json", Entry: exp.Name, Want: locked.Commit, Got: "stale", Repairable: true}}, nil
		}
		sources, readErr := os.ReadDir(filepath.Join(root, "dist-newstyle", "src"))
		if os.IsNotExist(readErr) {
			return []adapter.Finding{{File: "dist-newstyle/src", Entry: exp.Name, Want: locked.Commit, Got: "missing", Repairable: true}}, nil
		} else if readErr != nil {
			return nil, readErr
		}
		got := "missing"
		for _, source := range sources {
			if !source.IsDir() {
				continue
			}
			checkout := filepath.Join(root, "dist-newstyle", "src", source.Name())
			remote, remoteErr := hackageGitRemote(checkout)
			if remoteErr != nil && !os.IsNotExist(remoteErr) {
				return nil, remoteErr
			}
			if gitx.NormalizeURL(remote) != gitx.NormalizeURL(locked.Git) {
				continue
			}
			commit, headErr := hackageGitHead(checkout)
			if headErr != nil && !os.IsNotExist(headErr) {
				return nil, headErr
			}
			if commit == locked.Commit {
				return nil, nil
			}
			if commit != "" {
				got = commit
			}
		}
		return []adapter.Finding{{File: "dist-newstyle/src", Entry: exp.Name, Want: locked.Commit, Got: got, Repairable: true}}, nil
	}
	return nil, nil
}

func hackageGitHead(root string) (string, error) {
	gitDir, err := hackageGitDir(root)
	if err != nil {
		return "", err
	}
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(head))
	if !strings.HasPrefix(value, "ref: ") {
		return value, nil
	}
	ref := strings.TrimSpace(strings.TrimPrefix(value, "ref: "))
	refBody, err := os.ReadFile(filepath.Join(gitDir, filepath.FromSlash(ref)))
	if err == nil {
		return strings.TrimSpace(string(refBody)), nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	packed, err := os.ReadFile(filepath.Join(gitDir, "packed-refs"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(packed), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == ref {
			return fields[0], nil
		}
	}
	return "", nil
}

func hackageGitRemote(root string) (string, error) {
	gitDir, err := hackageGitDir(root)
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(filepath.Join(gitDir, "config"))
	if err != nil {
		return "", err
	}
	section := regexp.MustCompile(`(?ms)^\[remote "origin"\]\s*\n(.*?)(?:^\[|\z)`).FindSubmatch(body)
	if len(section) != 2 {
		return "", nil
	}
	match := regexp.MustCompile(`(?m)^[ \t]*url[ \t]*=[ \t]*(.+?)[ \t]*$`).FindSubmatch(section[1])
	if len(match) != 2 {
		return "", nil
	}
	return string(match[1]), nil
}

func hackageGitDir(root string) (string, error) {
	path := filepath.Join(root, ".git")
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return path, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(body))
	if !strings.HasPrefix(line, "gitdir: ") {
		return "", fmt.Errorf("%s: invalid gitdir file", path)
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(line, "gitdir: "))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(root, gitDir)
	}
	return filepath.Clean(gitDir), nil
}

func hackageMaterialize(ctx context.Context, root string, variant adapter.Variant) error {
	switch variant {
	case "stack":
		return adapter.Command(ctx, root, "stack", "build")
	case "cabal":
		return adapter.Command(ctx, root, "cabal", "build", "all")
	default:
		return fmt.Errorf("hackage: unsupported saved variant %q", variant)
	}
}

func hackagePlan(ctx context.Context, root string, variant adapter.Variant) error {
	switch variant {
	case "stack":
		return adapter.Command(ctx, root, "stack", "build", "--dry-run")
	case "cabal":
		return adapter.Command(ctx, root, "cabal", "build", "all", "--dry-run")
	default:
		return fmt.Errorf("hackage: unsupported saved variant %q", variant)
	}
}

var _ adapter.Adapter = Adapter{}
