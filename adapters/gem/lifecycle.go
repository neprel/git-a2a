package gem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/neprel/git-a2a/internal/adapter"
	"github.com/neprel/git-a2a/internal/gitx"
)

// Capability performs the source-shape portion of lifecycle preflight.
func (Adapter) Capability(root string, dep adapter.Dependency, exp adapter.Export) error {
	body, err := os.ReadFile(filepath.Join(root, "Gemfile"))
	if err != nil {
		return err
	}
	line := fmt.Sprintf("gem %s, git: %s, ref: %s", strconv.Quote(exp.Name), strconv.Quote(dep.Git), strconv.Quote(strings.Repeat("0", 40)))
	if exp.Path != "" {
		line += ", glob: " + strconv.Quote(strings.TrimSuffix(exp.Path, "/")+"/*.gemspec")
	}
	_, _ = upsert(string(body), exp.Name, line)
	return nil
}

// Pull converges both the native declaration and its local materialization.
func (a Adapter) Pull(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	change, err := a.Wire(ctx, root, dep, exp, locked)
	if err != nil {
		return change, err
	}
	return change, sync(ctx, root)
}

// Remove converges declaration, native lock state, and local materialization.
func (a Adapter) Remove(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	change, err := a.Unwire(ctx, root, dep, exp)
	if err != nil {
		return change, err
	}
	return change, sync(ctx, root)
}

// Inspect is local and read-only.
func (a Adapter) Inspect(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	findings, err := a.Drift(ctx, root, dep, exp, locked)
	if err != nil || len(findings) != 0 {
		return findings, err
	}
	body, err := os.ReadFile(filepath.Join(root, "Gemfile.lock"))
	if os.IsNotExist(err) {
		return []adapter.Finding{{File: "Gemfile.lock", Entry: exp.Name, Want: locked.Commit, Got: "missing", Repairable: true}}, nil
	}
	if err != nil {
		return nil, err
	}
	revision, ok := bundledGitRevision(body, locked.Git, exp.Name)
	if !ok || revision != locked.Commit {
		return []adapter.Finding{{File: "Gemfile.lock", Entry: exp.Name, Want: locked.Commit, Got: revision, Repairable: true}}, nil
	}
	return nil, nil
}

func bundledGitRevision(body []byte, source, name string) (string, bool) {
	lines := strings.Split(string(body), "\n")
	for i := 0; i < len(lines); {
		if lines[i] != "GIT" {
			i++
			continue
		}
		end := i + 1
		for end < len(lines) && (strings.HasPrefix(lines[end], "  ") || strings.TrimSpace(lines[end]) == "") {
			end++
		}
		remote, revision, hasSpec := "", "", false
		inSpecs := false
		for _, line := range lines[i+1 : end] {
			trimmed := strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(trimmed, "remote: "):
				remote = strings.TrimSpace(strings.TrimPrefix(trimmed, "remote: "))
			case strings.HasPrefix(trimmed, "revision: "):
				revision = strings.TrimSpace(strings.TrimPrefix(trimmed, "revision: "))
			case trimmed == "specs:":
				inSpecs = true
			case inSpecs && strings.HasPrefix(strings.TrimSpace(line), name+" ("):
				hasSpec = true
			}
		}
		if gitx.NormalizeURL(remote) == gitx.NormalizeURL(source) && hasSpec {
			return revision, true
		}
		i = end
	}
	return "", false
}

func sync(ctx context.Context, root string) error {
	if err := adapter.RequireTool(ctx, "gem", "bundler"); err != nil {
		return err
	}
	return adapter.Command(ctx, root, "bundle", "install", "--quiet")
}

var _ adapter.Adapter = Adapter{}
