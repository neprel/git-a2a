package clojure

import (
	"context"
	"os"
	"os/user"
	"path/filepath"

	"github.com/neprel/git-a2a/v2/internal/adapter"
)

// Capability performs the source-shape portion of lifecycle preflight.
func (a Adapter) Capability(root string, _ adapter.Dependency, exp adapter.Export) error {
	if _, _, err := a.Detect(root); err != nil {
		return err
	}
	if !validLib(exp.Name) {
		return adapter.NotWirable("Clojure dependency names must be qualified lib symbols")
	}
	return nil
}

// Pull converges both the native declaration and its local materialization.
func (a Adapter) Pull(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	if err := a.Capability(root, dep, exp); err != nil {
		return adapter.Change{}, err
	}
	if err := adapter.RequireTool(ctx, "clojure", "tools-deps"); err != nil {
		return adapter.Change{}, err
	}
	change, err := a.Wire(ctx, root, dep, exp, locked)
	if err != nil {
		return change, err
	}
	return change, adapter.Command(ctx, root, "clj", "-P")
}

// Remove converges declaration, native lock state, and local materialization.
func (a Adapter) Remove(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	if err := adapter.RequireTool(ctx, "clojure", "tools-deps"); err != nil {
		return adapter.Change{}, err
	}
	change, err := a.Unwire(ctx, root, dep, exp)
	if err != nil {
		return change, err
	}
	return change, adapter.Command(ctx, root, "clj", "-P")
}

// Inspect is local and read-only.
func (a Adapter) Inspect(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	findings, err := a.Drift(ctx, root, dep, exp, locked)
	if err != nil || len(findings) != 0 {
		return findings, err
	}
	checkouts, err := gitlibCheckouts(exp.Name, locked.Commit)
	if err != nil {
		return nil, err
	}
	for _, checkout := range checkouts {
		if _, statErr := os.Stat(checkout); statErr == nil {
			return nil, nil
		} else if !os.IsNotExist(statErr) {
			return nil, statErr
		}
	}
	checkout := checkouts[0]
	return []adapter.Finding{{File: filepath.ToSlash(checkout), Entry: exp.Name, Want: locked.Commit, Got: "missing", Repairable: true}}, nil
}

// tools.deps runs on the JVM, whose user.home normally comes from the account
// database rather than an overridden HOME. Check that location first, while
// retaining HOME as a valid location for wrappers that set the JVM property.
// This stays offline and does not invoke clj merely to discover its cache.
func gitlibCheckouts(name, commit string) ([]string, error) {
	var homes []string
	if current, err := user.Current(); err == nil && current.HomeDir != "" {
		homes = append(homes, current.HomeDir)
	}
	home, err := os.UserHomeDir()
	if err != nil && len(homes) == 0 {
		return nil, err
	}
	if home != "" && (len(homes) == 0 || home != homes[0]) {
		homes = append(homes, home)
	}
	checkouts := make([]string, 0, len(homes))
	for _, candidate := range homes {
		checkouts = append(checkouts, filepath.Join(candidate, ".gitlibs", "libs", filepath.FromSlash(name), commit))
	}
	return checkouts, nil
}

var _ adapter.Adapter = Adapter{}
