package hex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/neprel/git-a2a/v2/internal/adapter"
)

// Capability performs the source-shape portion of lifecycle preflight.
func (Adapter) Capability(root string, dep adapter.Dependency, exp adapter.Export) error {
	body, err := os.ReadFile(filepath.Join(root, "mix.exs"))
	if err != nil {
		return err
	}
	line := fmt.Sprintf("    {%s, git: %s, ref: %s", atom(exp.Name), strconv.Quote(dep.Git), strconv.Quote(strings.Repeat("0", 40)))
	if exp.Path != "" && exp.Path != "." {
		line += ", sparse: " + strconv.Quote(exp.Path)
	}
	line += "}"
	if _, _, err = upsert(string(body), exp.Name, line); err != nil {
		return adapter.NotWirable(err.Error())
	}
	return nil
}

// Pull converges both the native declaration and its local materialization.
func (a Adapter) Pull(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	change, err := a.Wire(ctx, root, dep, exp, locked)
	if err != nil {
		return change, err
	}
	return change, pull(ctx, root)
}

// Remove converges declaration, native lock state, and local materialization.
func (a Adapter) Remove(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	change, err := a.Unwire(ctx, root, dep, exp)
	if err != nil {
		return change, err
	}
	if err = requireTool(ctx); err != nil {
		return change, err
	}
	if err = adapter.Command(ctx, root, "mix", "deps.clean", "--unused"); err != nil {
		return change, err
	}
	if err = adapter.Command(ctx, root, "mix", "deps.unlock", "--unused"); err != nil {
		return change, err
	}
	return change, adapter.Command(ctx, root, "mix", "deps.get")
}

// Inspect is local and read-only.
func (a Adapter) Inspect(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	findings, err := a.Drift(ctx, root, dep, exp, locked)
	if err != nil || len(findings) != 0 {
		return findings, err
	}
	lockBody, err := os.ReadFile(filepath.Join(root, "mix.lock"))
	if os.IsNotExist(err) {
		return []adapter.Finding{{File: "mix.lock", Entry: exp.Name, Want: locked.Commit, Got: "missing", Repairable: true}}, nil
	} else if err != nil {
		return nil, err
	}
	gotCommit := ""
	if start := strings.Index(string(lockBody), strconv.Quote(exp.Name)+":"); start >= 0 {
		line := string(lockBody[start:])
		if end := strings.IndexByte(line, '\n'); end >= 0 {
			line = line[:end]
		}
		if strings.Contains(line, "{:git,") {
			gotCommit = regexp.MustCompile(`[0-9a-fA-F]{40}`).FindString(line)
		}
	}
	if gotCommit != locked.Commit {
		return []adapter.Finding{{File: "mix.lock", Entry: exp.Name, Want: locked.Commit, Got: gotCommit, Repairable: true}}, nil
	}
	checkout := filepath.Join(root, "deps", exp.Name)
	checkoutCommit, err := gitCheckoutCommit(checkout)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if checkoutCommit != locked.Commit {
		got := checkoutCommit
		if os.IsNotExist(err) || got == "" {
			got = "missing"
		}
		return []adapter.Finding{{File: filepath.ToSlash(filepath.Join("deps", exp.Name)), Entry: exp.Name, Want: locked.Commit, Got: got, Repairable: true}}, nil
	}
	return nil, nil
}

func gitCheckoutCommit(root string) (string, error) {
	head, err := os.ReadFile(filepath.Join(root, ".git", "HEAD"))
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(head))
	if !strings.HasPrefix(value, "ref: ") {
		return value, nil
	}
	ref := strings.TrimSpace(strings.TrimPrefix(value, "ref: "))
	body, err := os.ReadFile(filepath.Join(root, ".git", filepath.FromSlash(ref)))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

func requireTool(ctx context.Context) error { return adapter.RequireTool(ctx, "hex", "mix") }

func pull(ctx context.Context, root string) error {
	if err := requireTool(ctx); err != nil {
		return err
	}
	return adapter.Command(ctx, root, "mix", "deps.get")
}

var _ adapter.Adapter = Adapter{}
