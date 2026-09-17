package composer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/neprel/git-a2a/v2/internal/adapter"
)

// Capability performs the source-shape portion of lifecycle preflight.
func (a Adapter) Capability(root string, _ adapter.Dependency, exp adapter.Export) error {
	if _, _, err := a.Detect(root); err != nil {
		return err
	}
	if exp.Path != "" && exp.Path != "." {
		return adapter.NotWirable("Composer VCS repositories require composer.json at the repository root")
	}
	return nil
}

// Pull converges both the native declaration and its local materialization.
func (a Adapter) Pull(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	if err := a.Capability(root, dep, exp); err != nil {
		return adapter.Change{}, err
	}
	if err := adapter.RequireTool(ctx, "composer", "composer"); err != nil {
		return adapter.Change{}, err
	}
	change, err := a.Wire(ctx, root, dep, exp, locked)
	if err != nil {
		return change, err
	}
	err = adapter.Command(ctx, root, "composer", "update", exp.Name, "--with-dependencies", "--no-interaction", "--no-plugins", "--no-scripts")
	return change, err
}

// Remove converges declaration, native lock state, and local materialization.
func (a Adapter) Remove(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	if err := adapter.RequireTool(ctx, "composer", "composer"); err != nil {
		return adapter.Change{}, err
	}
	// Let Composer remove the selected root package from composer.lock and vendor
	// while it is still addressable from composer.json. Unwire then removes the
	// repository entry that belongs to git-a2a.
	if err := adapter.Command(ctx, root, "composer", "remove", exp.Name, "--update-with-dependencies", "--no-interaction", "--no-plugins", "--no-scripts"); err != nil {
		return adapter.Change{}, err
	}
	change, err := a.Unwire(ctx, root, dep, exp)
	if err != nil {
		return change, err
	}
	return change, nil
}

// Inspect is local and read-only.
func (a Adapter) Inspect(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	findings, err := a.Drift(ctx, root, dep, exp, locked)
	if err != nil || len(findings) != 0 {
		return findings, err
	}
	lockPath := filepath.Join(root, "composer.lock")
	body, err := os.ReadFile(lockPath)
	if os.IsNotExist(err) {
		return []adapter.Finding{{File: "composer.lock", Entry: exp.Name, Want: locked.Commit, Got: "missing", Repairable: true}}, nil
	}
	if err != nil {
		return nil, err
	}
	var lock struct {
		Packages []struct {
			Name   string `json:"name"`
			Source struct {
				Reference string `json:"reference"`
			} `json:"source"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(body, &lock); err != nil {
		return nil, fmt.Errorf("composer.lock: %w", err)
	}
	got := ""
	for _, pkg := range lock.Packages {
		if pkg.Name == exp.Name {
			got = pkg.Source.Reference
			break
		}
	}
	if got != locked.Commit {
		return []adapter.Finding{{File: "composer.lock", Entry: exp.Name, Want: locked.Commit, Got: got, Repairable: true}}, nil
	}
	vendorDir, err := composerVendorDir(root)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(root, vendorDir, filepath.FromSlash(exp.Name))); os.IsNotExist(err) {
		return []adapter.Finding{{File: filepath.ToSlash(filepath.Join(vendorDir, exp.Name)), Entry: exp.Name, Want: "installed", Got: "missing", Repairable: true}}, nil
	} else if err != nil {
		return nil, err
	}
	return nil, nil
}

func composerVendorDir(root string) (string, error) {
	body, err := os.ReadFile(filepath.Join(root, "composer.json"))
	if err != nil {
		return "", err
	}
	var doc struct {
		Config struct {
			VendorDir string `json:"vendor-dir"`
		} `json:"config"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", err
	}
	if doc.Config.VendorDir == "" {
		return "vendor", nil
	}
	if filepath.IsAbs(doc.Config.VendorDir) {
		return "", fmt.Errorf("composer config.vendor-dir must be project-relative")
	}
	return filepath.Clean(doc.Config.VendorDir), nil
}

var _ adapter.Adapter = Adapter{}
