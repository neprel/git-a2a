package msbuild

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/neprel/git-a2a/internal/adapter"
)

// Capability performs the source-shape portion of lifecycle preflight.
func (a Adapter) Capability(root string, _ adapter.Dependency, exp adapter.Export) error {
	if err := checkoutPathCapability(exp.Path); err != nil {
		return err
	}
	ok, _, err := a.Detect(root)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("MSBuild consumer project is missing")
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
	if err = adapter.RequireTool(ctx, a.Ecosystem(), variant); err != nil {
		return adapter.Change{}, err
	}
	if _, err = checkoutProject(root, exp); err != nil {
		return adapter.Change{}, err
	}
	change, err := a.wire(ctx, root, dep, exp, locked)
	if err != nil {
		return change, err
	}
	return change, a.resolve(ctx, root)
}

// Remove converges declaration, native lock state, and local materialization.
func (a Adapter) Remove(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, _ adapter.Locked) (adapter.Change, error) {
	_, variant, err := a.Detect(root)
	if err != nil {
		return adapter.Change{}, err
	}
	if err = adapter.RequireTool(ctx, a.Ecosystem(), variant); err != nil {
		return adapter.Change{}, err
	}
	change, err := a.unwire(ctx, root, dep, exp)
	if err != nil {
		return change, err
	}
	return change, a.resolve(ctx, root)
}

// Inspect is local and read-only.
func (a Adapter) Inspect(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	return a.inspectDeclaration(ctx, root, dep, exp, locked)
}

func checkoutPathCapability(path string) error {
	clean := filepath.Clean(filepath.FromSlash(path))
	if path == "" || clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return adapter.NotWirable("MSBuild project integration requires a materialized source checkout path")
	}
	return nil
}

var _ adapter.Adapter = Adapter{}
