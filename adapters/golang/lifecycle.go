package golang

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/neprel/git-a2a/v2/internal/adapter"
)

// Capability performs a read-only check of the consumer and source shape. A
// NotWirable result is reserved for Git URLs that cannot be represented as a
// Go module source; missing consumer state remains an ordinary error.
func (a Adapter) Capability(root string, dep adapter.Dependency, exp adapter.Export) error {
	detected, _, err := a.Detect(root)
	if err != nil {
		return err
	}
	if !detected {
		return fmt.Errorf("golang: go.mod not found")
	}
	if strings.TrimSpace(exp.Name) == "" {
		return adapter.NotWirable("golang: export name must be a Go module path")
	}
	if _, err := sourceModule(dep.Git, exp.Path); err != nil {
		return adapter.NotWirable(err.Error())
	}
	return nil
}

// Pull converges both the native declaration and its local materialization.
func (a Adapter) Pull(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	if err := a.Capability(root, dep, exp); err != nil {
		return adapter.Change{}, err
	}
	change, err := a.Wire(ctx, root, dep, exp, locked)
	if err != nil {
		return change, err
	}
	if err := a.download(ctx, root, exp.Name); err != nil {
		return change, fmt.Errorf("golang: materialize %s: %w", exp.Name, err)
	}
	return change, nil
}

// Remove drops only the owned root directives. The Go module cache is shared
// across projects and go.sum is a checksum cache rather than a root lock, so
// deleting either would risk unrelated or transitively required modules.
func (a Adapter) Remove(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	if err := adapter.RequireTool(ctx, "golang", "go"); err != nil {
		return adapter.Change{}, err
	}
	return a.Unwire(ctx, root, dep, exp)
}

// Inspect validates the declaration first, then asks the Go toolchain in
// offline/read-only mode where the selected module is materialized.
func (a Adapter) Inspect(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	findings, err := a.Drift(ctx, root, dep, exp, locked)
	if err != nil || len(findings) != 0 {
		return findings, err
	}
	if _, err := adapter.CommandOutput(ctx, root, "go", "mod", "edit", "-json"); err != nil {
		return nil, fmt.Errorf("golang: validate go.mod: %w", err)
	}
	present, got, err := a.materialized(ctx, root, exp.Name)
	if err != nil {
		return nil, err
	}
	if !present {
		return []adapter.Finding{{File: "go.sum", Entry: exp.Name, Want: "downloaded module", Got: got, Repairable: true}}, nil
	}
	return nil, nil
}

func (a Adapter) download(ctx context.Context, root, module string) error {
	if a.Download != nil {
		return a.Download(ctx, root, module)
	}
	return adapter.Command(ctx, root, "go", "mod", "download", module)
}

func (a Adapter) materialized(ctx context.Context, root, module string) (bool, string, error) {
	if a.Materialized != nil {
		return a.Materialized(ctx, root, module)
	}
	if err := adapter.RequireTool(ctx, "golang", "go"); err != nil {
		return false, "", err
	}
	cmd := exec.CommandContext(ctx, "go", "list", "-m", "-json", "-mod=readonly", module)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GOPROXY=off", "GOSUMDB=off")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail == "" {
			detail = "module is not present in the local Go cache"
		}
		return false, detail, nil
	}
	var result struct {
		Dir     string
		Replace *struct{ Dir string }
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return false, "", fmt.Errorf("golang: decode local module state: %w", err)
	}
	dir := result.Dir
	if result.Replace != nil && result.Replace.Dir != "" {
		dir = result.Replace.Dir
	}
	if dir == "" {
		return false, "module has no local directory", nil
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return false, "local module directory is missing", nil
		}
		return false, "", fmt.Errorf("golang: inspect materialized module: %w", err)
	}
	return true, dir, nil
}

var _ adapter.Adapter = Adapter{}
