package pub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/neprel/git-a2a/internal/adapter"
)

// Capability performs the source-shape portion of lifecycle preflight.
func (Adapter) Capability(root string, dep adapter.Dependency, exp adapter.Export) error {
	body, err := os.ReadFile(filepath.Join(root, "pubspec.yaml"))
	if err != nil {
		return err
	}
	lines := []string{fmt.Sprintf("  %s:", exp.Name), "    git:", "      url: " + strconv.Quote(dep.Git), "      ref: " + strconv.Quote(strings.Repeat("0", 40))}
	if exp.Path != "" && exp.Path != "." {
		lines = append(lines, "      path: "+strconv.Quote(strings.Trim(exp.Path, "/")))
	}
	if _, _, err = upsert(string(body), exp.Name, strings.Join(lines, "\n")+"\n"); err != nil {
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
	lockBody, err := os.ReadFile(filepath.Join(root, "pubspec.lock"))
	if os.IsNotExist(err) {
		return []adapter.Finding{{File: "pubspec.lock", Entry: exp.Name, Want: locked.Commit, Got: "missing", Repairable: true}}, nil
	} else if err != nil {
		return nil, err
	}
	gotCommit := pubLockedCommit(string(lockBody), exp.Name)
	if gotCommit != locked.Commit {
		return []adapter.Finding{{File: "pubspec.lock", Entry: exp.Name, Want: locked.Commit, Got: gotCommit, Repairable: true}}, nil
	}
	configPath := filepath.Join(root, ".dart_tool", "package_config.json")
	configBody, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return []adapter.Finding{{File: ".dart_tool/package_config.json", Entry: exp.Name, Want: locked.Commit, Got: "missing", Repairable: true}}, nil
	} else if err != nil {
		return nil, err
	}
	materialized, err := pubPackageRoot(configPath, configBody, exp.Name)
	if err != nil {
		return nil, err
	}
	if materialized == "" {
		return []adapter.Finding{{File: ".dart_tool/package_config.json", Entry: exp.Name, Want: locked.Commit, Got: "missing", Repairable: true}}, nil
	}
	if _, err = os.Stat(materialized); os.IsNotExist(err) {
		return []adapter.Finding{{File: filepath.ToSlash(materialized), Entry: exp.Name, Want: locked.Commit, Got: "missing", Repairable: true}}, nil
	} else if err != nil {
		return nil, err
	}
	return nil, nil
}

func pubLockedCommit(document, name string) string {
	entry := regexp.MustCompile(`(?ms)^  ` + regexp.QuoteMeta(name) + `:\n(.*?)(?:^  [^ \n][^:]*:\n|\z)`).FindStringSubmatch(document)
	if len(entry) != 2 {
		return ""
	}
	match := regexp.MustCompile(`(?m)^      resolved-ref: ["']?([^"' #\n]+)`).FindStringSubmatch(entry[1])
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func pubPackageRoot(configPath string, body []byte, name string) (string, error) {
	var config struct {
		Packages []struct {
			Name    string `json:"name"`
			RootURI string `json:"rootUri"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(body, &config); err != nil {
		return "", fmt.Errorf(".dart_tool/package_config.json: %w", err)
	}
	for _, pkg := range config.Packages {
		if pkg.Name != name {
			continue
		}
		parsed, err := url.Parse(pkg.RootURI)
		if err != nil {
			return "", err
		}
		if parsed.Scheme == "file" {
			path := parsed.Path
			if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
				path = path[1:]
			}
			return filepath.FromSlash(path), nil
		}
		if parsed.Scheme != "" {
			return "", fmt.Errorf("package %s has unsupported root URI %q", name, pkg.RootURI)
		}
		return filepath.Clean(filepath.Join(filepath.Dir(configPath), filepath.FromSlash(pkg.RootURI))), nil
	}
	return "", nil
}

func sync(ctx context.Context, root string) error {
	if err := adapter.RequireTool(ctx, "pub", "pub"); err != nil {
		return err
	}
	return adapter.Command(ctx, root, "dart", "pub", "get")
}

var _ adapter.Adapter = Adapter{}
