package maven

import (
	"context"
	"errors"
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
	if _, err := parseCoordinate(exp.Name); err != nil {
		return adapter.NotWirable(err.Error())
	}
	ok, _, err := a.Detect(root)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("Maven consumer marker %s is missing", rootFile)
	}
	return nil
}

// Pull converges both the native declaration and its local materialization.
func (a Adapter) Pull(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	if err := a.Capability(root, dep, exp); err != nil {
		return adapter.Change{}, err
	}
	if err := adapter.RequireTool(ctx, a.Ecosystem(), "maven"); err != nil {
		return adapter.Change{}, err
	}
	if err := requireCheckoutModule(root, exp.Path); err != nil {
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
	if err := adapter.RequireTool(ctx, a.Ecosystem(), "maven"); err != nil {
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
	if err = requireCheckoutModule(root, exp.Path); err != nil {
		findings = append(findings, adapter.Finding{File: exp.Path, Entry: dep.Name, Want: "materialized Maven module", Got: err.Error(), Repairable: errors.Is(err, os.ErrNotExist)})
	}
	return findings, nil
}

func checkoutPathCapability(path string) error {
	clean := filepath.Clean(filepath.FromSlash(path))
	if path == "" || clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return adapter.NotWirable("Maven reactor integration requires a materialized source checkout path")
	}
	return nil
}

func requireCheckoutModule(root, path string) error {
	module := filepath.Join(root, filepath.FromSlash(path))
	info, err := os.Stat(module)
	if err != nil {
		return fmt.Errorf("Maven module %s: %w", path, err)
	}
	if info.IsDir() {
		module = filepath.Join(module, rootFile)
	} else if filepath.Base(module) != rootFile {
		return fmt.Errorf("Maven module %s is not a directory or pom.xml", path)
	}
	info, err = os.Stat(module)
	if err != nil {
		return fmt.Errorf("Maven module %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("Maven module %s does not contain a pom.xml file", path)
	}
	return nil
}

var _ adapter.Adapter = Adapter{}
