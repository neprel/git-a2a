package cargo

import (
	"bytes"
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
	body, err := os.ReadFile(filepath.Join(root, "Cargo.toml"))
	if err != nil {
		return err
	}
	line := fmt.Sprintf("%s = { git = %q, rev = %q }", tomlKey(exp.Name), dep.Git, "0000000000000000000000000000000000000000")
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
	body, err := os.ReadFile(filepath.Join(root, "Cargo.lock"))
	if os.IsNotExist(err) {
		return []adapter.Finding{{File: "Cargo.lock", Entry: exp.Name, Want: locked.Commit, Got: "missing", Repairable: true}}, nil
	}
	if err != nil {
		return nil, err
	}
	if source, ok := cargoLockedSource(body, exp.Name, locked.Commit); !ok {
		return []adapter.Finding{{File: "Cargo.lock", Entry: exp.Name, Want: locked.Commit, Got: source, Repairable: true}}, nil
	}
	return nil, nil
}

func cargoLockedSource(body []byte, name, commit string) (string, bool) {
	nameLine := regexp.MustCompile(`(?m)^name\s*=\s*` + regexp.QuoteMeta(strconv.Quote(name)) + `\s*$`)
	sourceLine := regexp.MustCompile(`(?m)^source\s*=\s*"([^"]+)"\s*$`)
	var candidates []string
	// Splitting keeps every stanza header independent. A regexp whose match
	// terminator consumes the next [[package]] header skips every second stanza.
	for _, block := range bytes.Split(body, []byte("[[package]]"))[1:] {
		if !nameLine.Match(block) {
			continue
		}
		source := sourceLine.FindSubmatch(block)
		if len(source) == 2 {
			candidate := string(source[1])
			candidates = append(candidates, candidate)
			if strings.Contains(candidate, commit) {
				return candidate, true
			}
		}
	}
	return strings.Join(candidates, ", "), false
}

func sync(ctx context.Context, root string) error {
	if err := adapter.RequireTool(ctx, "cargo", "cargo"); err != nil {
		return err
	}
	return adapter.Command(ctx, root, "cargo", "fetch")
}

var _ adapter.Adapter = Adapter{}
