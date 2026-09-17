package zig

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/neprel/git-a2a/v2/internal/adapter"
)

// Capability performs the source-shape portion of lifecycle preflight.
func (a Adapter) Capability(root string, dep adapter.Dependency, exp adapter.Export) error {
	if _, _, err := a.Detect(root); err != nil {
		return err
	}
	if exp.Path != "" && exp.Path != "." {
		return adapter.NotWirable("Zig package URLs cannot select a repository subdirectory")
	}
	if strings.TrimSpace(exp.Checksum) == "" {
		// Durable bindings retain the selected source shape (adapter, export and
		// path), while the content hash is read from the upstream export at each
		// transaction. Permit the read-only saved-binding preflight here; Pull
		// receives that full export and checks the checksum again before mutation.
		for _, binding := range dep.Bindings {
			if binding.Adapter == a.Ecosystem() && binding.Export == exp.Name {
				return nil
			}
		}
		return adapter.NotWirable("Zig requires exports[].checksum for package integrity")
	}
	return nil
}

// Pull converges both the native declaration and its local materialization.
func (a Adapter) Pull(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	if err := a.Capability(root, dep, exp); err != nil {
		return adapter.Change{}, err
	}
	if err := adapter.RequireTool(ctx, "zig", "zon"); err != nil {
		return adapter.Change{}, err
	}
	change, err := a.Wire(ctx, root, dep, exp, locked)
	if err != nil {
		return change, err
	}
	return change, adapter.Command(ctx, root, "zig", "build", "--fetch")
}

// Remove converges declaration, native lock state, and local materialization.
func (a Adapter) Remove(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	if err := adapter.RequireTool(ctx, "zig", "zon"); err != nil {
		return adapter.Change{}, err
	}
	change, err := a.Unwire(ctx, root, dep, exp)
	if err != nil {
		return change, err
	}
	return change, adapter.Command(ctx, root, "zig", "build", "--fetch")
}

// Inspect is local and read-only.
func (a Adapter) Inspect(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	findings, err := a.Drift(ctx, root, dep, exp, locked)
	if err != nil || len(findings) != 0 {
		return findings, err
	}
	body, err := os.ReadFile(filepath.Join(root, "build.zig.zon"))
	if err != nil {
		return nil, err
	}
	block := managedBlock(exp.Name).Find(body)
	match := regexp.MustCompile(`\.hash\s*=\s*"([^"]+)"`).FindSubmatch(block)
	if len(match) != 2 || filepath.Base(string(match[1])) != string(match[1]) {
		return []adapter.Finding{{File: "build.zig.zon", Entry: exp.Name, Want: "valid Zig package hash", Got: strings.TrimSpace(string(block))}}, nil
	}
	hash := string(match[1])
	if exp.Checksum != "" && hash != exp.Checksum {
		return []adapter.Finding{{File: "build.zig.zon", Entry: exp.Name, Want: exp.Checksum, Got: hash}}, nil
	}
	cache, err := zigGlobalCacheDir()
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(cache) {
		cache = filepath.Join(root, cache)
	}
	target := filepath.Join(cache, "p", hash)
	if _, err = os.Stat(target); os.IsNotExist(err) {
		return []adapter.Finding{{File: filepath.ToSlash(target), Entry: exp.Name, Want: hash, Got: "missing", Repairable: true}}, nil
	}
	return nil, err
}

func zigGlobalCacheDir() (string, error) {
	if configured := os.Getenv("ZIG_GLOBAL_CACHE_DIR"); configured != "" {
		return configured, nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "zig"), nil
}

var _ adapter.Adapter = Adapter{}
