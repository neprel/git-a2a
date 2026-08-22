package golang

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
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

func (Adapter) Wire(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
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
	args := []string{"mod", "edit"}
	if source == exp.Name {
		args = append(args, "-require="+exp.Name+"@"+version, "-dropreplace="+exp.Name)
	} else {
		args = append(args, "-require="+exp.Name+"@v0.0.0", "-replace="+exp.Name+"="+source+"@"+version)
	}
	if err := adapter.Command(ctx, root, "go", args...); err != nil {
		return adapter.Change{}, err
	}
	next, err := os.ReadFile(p)
	changed := string(next) != s
	return adapter.Change{File: "go.mod", Entry: exp.Name, Changed: changed}, err
}

func (Adapter) Unwire(ctx context.Context, root string, _ adapter.Dependency, exp adapter.Export) (adapter.Change, error) {
	p := filepath.Join(root, "go.mod")
	b, err := os.ReadFile(p)
	if err != nil {
		return adapter.Change{}, err
	}
	s := string(b)
	if err := adapter.Command(ctx, root, "go", "mod", "edit", "-droprequire="+exp.Name, "-dropreplace="+exp.Name); err != nil {
		return adapter.Change{}, err
	}
	next, err := os.ReadFile(p)
	changed := string(next) != s
	return adapter.Change{File: "go.mod", Entry: exp.Name, Changed: changed}, err
}
func (Adapter) Refresh(ctx context.Context, root string, _ adapter.Dependency, _ adapter.Export, _ adapter.Locked) error {
	return nil
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
	badPin := !strings.Contains(line, prefix)
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
func findLine(s, kind, name string) string {
	inBlock := false
	for _, line := range strings.Split(s, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == kind && fields[1] == "(" {
			inBlock = true
			continue
		}
		if inBlock && len(fields) == 1 && fields[0] == ")" {
			inBlock = false
			continue
		}
		if inBlock && len(fields) >= 2 && fields[0] == name {
			return line
		}
		if !inBlock && len(fields) >= 3 && fields[0] == kind && fields[1] == name {
			return line
		}
	}
	return ""
}
