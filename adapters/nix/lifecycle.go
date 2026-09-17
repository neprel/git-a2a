package nix

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/neprel/git-a2a/internal/adapter"
)

// Capability performs the source-shape portion of lifecycle preflight.
func (a Adapter) Capability(root string, dep adapter.Dependency, exp adapter.Export) error {
	if _, _, err := a.Detect(root); err != nil {
		return err
	}
	if _, err := flakeURL(dep, exp, adapter.Locked{Commit: "0000000000000000000000000000000000000000"}); err != nil {
		return adapter.NotWirable(err.Error())
	}
	return nil
}

// Pull converges both the native declaration and its local materialization.
func (a Adapter) Pull(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	if err := a.Capability(root, dep, exp); err != nil {
		return adapter.Change{}, err
	}
	if err := adapter.RequireTool(ctx, "nix", "flake"); err != nil {
		return adapter.Change{}, err
	}
	change, err := a.Wire(ctx, root, dep, exp, locked)
	if err != nil {
		return change, err
	}
	return change, adapter.Command(ctx, root, "nix", "flake", "lock", "--update-input", exp.Name)
}

// Remove converges declaration, native lock state, and local materialization.
func (a Adapter) Remove(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	if err := adapter.RequireTool(ctx, "nix", "flake"); err != nil {
		return adapter.Change{}, err
	}
	change, err := a.Unwire(ctx, root, dep, exp)
	if err != nil {
		return change, err
	}
	return change, adapter.Command(ctx, root, "nix", "flake", "lock")
}

// Inspect is local and read-only.
func (a Adapter) Inspect(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	findings, err := a.Drift(ctx, root, dep, exp, locked)
	if err != nil || len(findings) != 0 {
		return findings, err
	}
	body, err := os.ReadFile(filepath.Join(root, "flake.lock"))
	if os.IsNotExist(err) {
		return []adapter.Finding{{File: "flake.lock", Entry: exp.Name, Want: locked.Commit, Got: "missing", Repairable: true}}, nil
	}
	if err != nil {
		return nil, err
	}
	var lock struct {
		Nodes map[string]struct {
			Inputs map[string]json.RawMessage `json:"inputs"`
			Locked struct {
				Rev string `json:"rev"`
			} `json:"locked"`
		} `json:"nodes"`
		Root string `json:"root"`
	}
	if err := json.Unmarshal(body, &lock); err != nil {
		return nil, fmt.Errorf("flake.lock: %w", err)
	}
	rootNode, ok := lock.Nodes[lock.Root]
	if !ok {
		return []adapter.Finding{{File: "flake.lock", Entry: exp.Name, Want: locked.Commit, Got: "missing root node", Repairable: true}}, nil
	}
	var nodeName string
	if raw := rootNode.Inputs[exp.Name]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &nodeName)
	}
	got := lock.Nodes[nodeName].Locked.Rev
	if got != locked.Commit {
		return []adapter.Finding{{File: "flake.lock", Entry: exp.Name, Want: locked.Commit, Got: got, Repairable: true}}, nil
	}
	return nil, nil
}

var _ adapter.Adapter = Adapter{}
