package hackage

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/neprel/git-a2a/internal/adapter"
	"github.com/neprel/git-a2a/internal/gitx"
)

type Adapter struct{}

func (Adapter) Ecosystem() string { return "hackage" }

func (Adapter) Detect(root string) (bool, adapter.Variant, error) {
	if _, err := os.Stat(filepath.Join(root, "stack.yaml")); err == nil {
		return true, "stack", nil
	} else if !os.IsNotExist(err) {
		return false, "", err
	}
	_, err := os.Stat(filepath.Join(root, "cabal.project"))
	if os.IsNotExist(err) {
		return false, "", nil
	}
	return err == nil, "cabal", err
}

func (a Adapter) Wire(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	ok, variant, err := a.Detect(root)
	if err != nil || !ok {
		return adapter.Change{}, err
	}
	file := "cabal.project"
	comment := "--"
	block := cabalBlock(dep, exp, locked)
	if variant == "stack" {
		file, comment = "stack.yaml", "#"
		block = stackBlock(dep, exp, locked)
	}
	path := filepath.Join(root, file)
	body, err := os.ReadFile(path)
	if err != nil {
		return adapter.Change{}, err
	}
	next, changed, err := upsert(string(body), exp.Name, comment, block, variant)
	if err == nil && changed {
		err = os.WriteFile(path, []byte(next), 0o644)
	}
	return adapter.Change{File: file, Entry: exp.Name, Changed: changed}, err
}

func (a Adapter) Unwire(_ context.Context, root string, _ adapter.Dependency, exp adapter.Export) (adapter.Change, error) {
	ok, variant, err := a.Detect(root)
	if err != nil || !ok {
		return adapter.Change{}, err
	}
	file, comment := "cabal.project", "--"
	if variant == "stack" {
		file, comment = "stack.yaml", "#"
	}
	path := filepath.Join(root, file)
	body, err := os.ReadFile(path)
	if err != nil {
		return adapter.Change{}, err
	}
	re := managedBlock(exp.Name, comment)
	loc := re.FindIndex(body)
	if loc == nil {
		return adapter.Change{File: file, Entry: exp.Name}, nil
	}
	if variant == "stack" {
		created := regexp.MustCompile(`(?m)^extra-deps:[ \t]*# git-a2a:created ` + regexp.QuoteMeta(exp.Name) + `\n`)
		if header := created.FindIndex(body); header != nil {
			loc[0] = header[0]
		}
	}
	if variant == "cabal" && loc[0] > 0 && body[loc[0]-1] == '\n' {
		loc[0]--
	}
	next := string(body[:loc[0]]) + string(body[loc[1]:])
	err = os.WriteFile(path, []byte(next), 0o644)
	return adapter.Change{File: file, Entry: exp.Name, Changed: true}, err
}

func (a Adapter) Refresh(ctx context.Context, root string, _ adapter.Dependency, _ adapter.Export, _ adapter.Locked) error {
	_, variant, err := a.Detect(root)
	if err != nil {
		return err
	}
	return adapter.RequireTool(ctx, "hackage", variant)
}

func (a Adapter) Drift(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	ok, variant, err := a.Detect(root)
	if err != nil || !ok {
		return nil, err
	}
	file, comment := "cabal.project", "--"
	if variant == "stack" {
		file, comment = "stack.yaml", "#"
	}
	body, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		return nil, err
	}
	block := managedBlock(exp.Name, comment).FindString(string(body))
	gotURL := field(block, "location")
	if variant == "stack" {
		gotURL = field(block, "git")
	}
	badPin := dep.Track != "floating" && !strings.Contains(block, locked.Commit)
	if block == "" || gitx.NormalizeURL(gotURL) != gitx.NormalizeURL(locked.Git) || badPin {
		return []adapter.Finding{{File: file, Entry: exp.Name, Want: locked.Commit, Got: strings.TrimSpace(block)}}, nil
	}
	return nil, nil
}

func cabalBlock(dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) string {
	pinField, pin := "tag", locked.Commit
	if dep.Track == "floating" {
		pinField, pin = "branch", dep.Ref
	}
	lines := []string{
		"source-repository-package",
		"    type: git",
		"    location: " + dep.Git,
		"    " + pinField + ": " + pin,
	}
	if exp.Path != "" && exp.Path != "." {
		lines = append(lines, "    subdir: "+exp.Path)
	}
	return strings.Join(lines, "\n")
}

func stackBlock(dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) string {
	pin := locked.Commit
	if dep.Track == "floating" {
		pin = dep.Ref
	}
	lines := []string{"- git: " + dep.Git, "  commit: " + pin}
	if exp.Path != "" && exp.Path != "." {
		lines = append(lines, "  subdirs:", "  - "+exp.Path)
	}
	return strings.Join(lines, "\n")
}

func upsert(document, name, comment, block string, variant adapter.Variant) (string, bool, error) {
	managed := comment + " git-a2a:begin " + name + "\n" + block + "\n" + comment + " git-a2a:end " + name + "\n"
	re := managedBlock(name, comment)
	if old := re.FindString(document); old != "" {
		if old == managed {
			return document, false, nil
		}
		return re.ReplaceAllStringFunc(document, func(string) string { return managed }), true, nil
	}
	if variant != "stack" {
		separator := "\n"
		if strings.HasSuffix(document, "\n") {
			separator = ""
		}
		return document + separator + "\n" + managed, true, nil
	}
	start, end, ok := yamlListRange(document, "extra-deps")
	if !ok {
		separator := "\n"
		if strings.HasSuffix(document, "\n") {
			separator = ""
		}
		return document + separator + "extra-deps: # git-a2a:created " + name + "\n" + managed, true, nil
	}
	_ = start
	return document[:end] + managed + document[end:], true, nil
}

func managedBlock(name, comment string) *regexp.Regexp {
	return regexp.MustCompile(`(?ms)^[ \t]*` + regexp.QuoteMeta(comment+" git-a2a:begin "+name) + `\n.*?^[ \t]*` + regexp.QuoteMeta(comment+" git-a2a:end "+name) + `\n`)
}

func yamlListRange(document, key string) (int, int, bool) {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:[ \t]*(?:#.*)?\n`)
	loc := re.FindStringIndex(document)
	if loc == nil {
		return 0, 0, false
	}
	end := len(document)
	if next := regexp.MustCompile(`(?m)^[A-Za-z0-9_-]+:`).FindStringIndex(document[loc[1]:]); next != nil {
		end = loc[1] + next[0]
	}
	return loc[1], end, true
}

func field(block, name string) string {
	match := regexp.MustCompile(`(?m)^[ \t-]*` + regexp.QuoteMeta(name) + `:[ \t]*([^\n#]+)`).FindStringSubmatch(block)
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}
