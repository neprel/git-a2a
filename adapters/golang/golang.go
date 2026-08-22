package golang

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/neprel/git-a2a/internal/adapter"
)

type Adapter struct{}

func (Adapter) Ecosystem() string { return "golang" }
func (Adapter) Detect(root string) (bool, adapter.Variant, error) {
	_, err := os.Stat(filepath.Join(root, "go.mod"))
	if os.IsNotExist(err) {
		return false, "", nil
	}
	return err == nil, "go", err
}

func (Adapter) Wire(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	p := filepath.Join(root, "go.mod")
	b, err := os.ReadFile(p)
	if err != nil {
		return adapter.Change{}, err
	}
	s := string(b)
	source, err := sourceModule(dep.Git, exp.Path)
	if err != nil {
		return adapter.Change{}, adapter.NotWirable(err.Error())
	}
	version := "v0.0.0-00010101000000-" + locked.Commit[:12]
	if dep.Track == "floating" {
		version = dep.Ref
	}
	next := upsertLine(s, "require", exp.Name, fmt.Sprintf("require %s v0.0.0", exp.Name))
	if source == exp.Name {
		next = upsertLine(next, "require", exp.Name, fmt.Sprintf("require %s %s", exp.Name, version))
		next = removeLine(next, "replace", exp.Name)
	} else {
		next = upsertLine(next, "replace", exp.Name, fmt.Sprintf("replace %s => %s %s", exp.Name, source, version))
	}
	changed := next != s
	if changed {
		err = os.WriteFile(p, []byte(next), 0o644)
	}
	return adapter.Change{File: "go.mod", Entry: exp.Name, Changed: changed}, err
}

func (Adapter) Unwire(_ context.Context, root string, _ adapter.Dependency, exp adapter.Export) (adapter.Change, error) {
	p := filepath.Join(root, "go.mod")
	b, err := os.ReadFile(p)
	if err != nil {
		return adapter.Change{}, err
	}
	s := string(b)
	next := removeLine(removeLine(s, "require", exp.Name), "replace", exp.Name)
	changed := next != s
	if changed {
		err = os.WriteFile(p, []byte(next), 0o644)
	}
	return adapter.Change{File: "go.mod", Entry: exp.Name, Changed: changed}, err
}
func (Adapter) Refresh(ctx context.Context, root string, _ adapter.Dependency, _ adapter.Export, _ adapter.Locked) error {
	return adapter.Command(ctx, root, "go", "mod", "tidy")
}
func (Adapter) Drift(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, err
	}
	prefix := locked.Commit
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	line := findLine(string(b), "replace", exp.Name)
	if line == "" {
		line = findLine(string(b), "require", exp.Name)
	}
	source, _ := sourceModule(locked.Git, exp.Path)
	badURL := source != "" && !strings.Contains(strings.ToLower(line), strings.ToLower(source))
	badPin := dep.Track != "floating" && !strings.Contains(line, prefix)
	if badURL || badPin {
		return []adapter.Finding{{File: "go.mod", Entry: exp.Name, Want: prefix, Got: strings.TrimSpace(line)}}, nil
	}
	return nil, nil
}

func sourceModule(raw, path string) (string, error) {
	var hostPath string
	if strings.HasPrefix(raw, "git@") {
		parts := strings.SplitN(strings.TrimPrefix(raw, "git@"), ":", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("unsupported git URL %q", raw)
		}
		hostPath = parts[0] + "/" + parts[1]
	} else {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return "", fmt.Errorf("cannot derive Go module source from %q", raw)
		}
		hostPath = u.Host + u.Path
	}
	hostPath = strings.TrimSuffix(hostPath, ".git")
	if path != "" && path != "." {
		hostPath += "/" + strings.Trim(path, "/")
	}
	return hostPath, nil
}
func linePattern(kind, name string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^\s*` + kind + `\s+` + regexp.QuoteMeta(name) + `(?:\s|$).*$`)
}
func upsertLine(s, kind, name, line string) string {
	re := linePattern(kind, name)
	if re.MatchString(s) {
		return re.ReplaceAllString(s, line)
	}
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s + line + "\n"
}
func removeLine(s, kind, name string) string { return linePattern(kind, name).ReplaceAllString(s, "") }
func findLine(s, kind, name string) string   { return linePattern(kind, name).FindString(s) }
