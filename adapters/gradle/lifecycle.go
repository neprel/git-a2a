package gradle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/neprel/git-a2a/internal/adapter"
)

// Capability performs the source-shape portion of lifecycle preflight.
func (a Adapter) Capability(root string, _ adapter.Dependency, exp adapter.Export) error {
	if err := checkoutPathCapability(exp.Path); err != nil {
		return err
	}
	parts := strings.Split(exp.Name, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return adapter.NotWirable("Gradle composite integration requires a Maven group:artifact export name")
	}
	ok, _, err := a.Detect(root)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("Gradle consumer settings file is missing")
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
	if err = requireCheckoutDirectory(root, exp.Path); err != nil {
		return adapter.Change{}, err
	}
	change, err := a.wire(ctx, root, dep, exp, locked)
	if err != nil {
		return change, err
	}
	return change, a.resolve(ctx, root)
}

// Remove converges declaration, native lock state, and local materialization.
func (a Adapter) Remove(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
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
	findings, err := a.inspectDeclaration(ctx, root, dep, exp, locked)
	if err != nil {
		return nil, err
	}
	if info, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(exp.Path))); statErr != nil || !info.IsDir() {
		got := "not a directory"
		if os.IsNotExist(statErr) {
			got = "missing"
		} else if statErr != nil {
			return nil, statErr
		}
		findings = append(findings, adapter.Finding{File: exp.Path, Entry: dep.Name, Want: "materialized composite build directory", Got: got, Repairable: got == "missing"})
	}
	return findings, nil
}

func checkoutPathCapability(path string) error {
	clean := filepath.Clean(filepath.FromSlash(path))
	if path == "" || clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return adapter.NotWirable("Gradle composite integration requires a materialized source checkout path")
	}
	return nil
}

func requireCheckoutDirectory(root, path string) error {
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return fmt.Errorf("composite build %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("composite build %s is not a directory", path)
	}
	return nil
}

var _ adapter.Adapter = Adapter{}
