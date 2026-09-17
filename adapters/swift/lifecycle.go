package swift

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/neprel/git-a2a/v2/internal/adapter"
	"github.com/neprel/git-a2a/v2/internal/gitx"
)

// Capability performs the source-shape portion of lifecycle preflight.
func (Adapter) Capability(root string, dep adapter.Dependency, exp adapter.Export) error {
	if exp.Path != "" && exp.Path != "." {
		return adapter.NotWirable("swiftpm git dependencies cannot address a package below the repository root")
	}
	body, err := os.ReadFile(filepath.Join(root, "Package.swift"))
	if err != nil {
		return err
	}
	entry := fmt.Sprintf(".package(url: %s, revision: %s)", strconv.Quote(dep.Git), strconv.Quote(strings.Repeat("0", 40)))
	if _, _, err = upsert(string(body), exp.Name, dep.Git, entry); err != nil {
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
	body, err := os.ReadFile(filepath.Join(root, "Package.resolved"))
	if os.IsNotExist(err) {
		return []adapter.Finding{{File: "Package.resolved", Entry: exp.Name, Want: locked.Commit, Got: "missing", Repairable: true}}, nil
	}
	if err != nil {
		return nil, err
	}
	revision, ok, err := swiftLockedRevision(body, locked.Git)
	if err != nil {
		return nil, fmt.Errorf("swift: decode Package.resolved: %w", err)
	}
	if !ok || revision != locked.Commit {
		return []adapter.Finding{{File: "Package.resolved", Entry: exp.Name, Want: locked.Commit, Got: revision, Repairable: true}}, nil
	}
	return nil, nil
}

func swiftLockedRevision(body []byte, source string) (string, bool, error) {
	var document struct {
		Pins []struct {
			Location   string `json:"location"`
			Repository string `json:"repositoryURL"`
			State      struct {
				Revision string `json:"revision"`
			} `json:"state"`
		} `json:"pins"`
		Object struct {
			Pins []struct {
				Location   string `json:"location"`
				Repository string `json:"repositoryURL"`
				State      struct {
					Revision string `json:"revision"`
				} `json:"state"`
			} `json:"pins"`
		} `json:"object"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		return "", false, err
	}
	pins := document.Pins
	if len(pins) == 0 {
		pins = document.Object.Pins
	}
	for _, pin := range pins {
		location := pin.Location
		if location == "" {
			location = pin.Repository
		}
		if gitx.NormalizeURL(location) == gitx.NormalizeURL(source) {
			return pin.State.Revision, true, nil
		}
	}
	return "", false, nil
}

func sync(ctx context.Context, root string) error {
	if err := adapter.RequireTool(ctx, "swift", "swiftpm"); err != nil {
		return err
	}
	return adapter.Command(ctx, root, "swift", "package", "resolve")
}

var _ adapter.Adapter = Adapter{}
