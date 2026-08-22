package gem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/neprel/git-a2a/internal/adapter"
	"github.com/neprel/git-a2a/internal/gitx"
)

type Adapter struct{}

func (Adapter) Ecosystem() string { return "gem" }

func (Adapter) Detect(root string) (bool, adapter.Variant, error) {
	_, err := os.Stat(filepath.Join(root, "Gemfile"))
	if os.IsNotExist(err) {
		return false, "", nil
	}
	return err == nil, "bundler", err
}

func (Adapter) Wire(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	path := filepath.Join(root, "Gemfile")
	body, err := os.ReadFile(path)
	if err != nil {
		return adapter.Change{}, err
	}
	pinKey, pin := "ref", locked.Commit
	if dep.Track == "floating" {
		pinKey, pin = "branch", dep.Ref
	}
	line := fmt.Sprintf("gem %s, git: %s, %s: %s", strconv.Quote(exp.Name), strconv.Quote(dep.Git), pinKey, strconv.Quote(pin))
	if exp.Path != "" {
		line += ", glob: " + strconv.Quote(strings.TrimSuffix(exp.Path, "/")+"/*.gemspec")
	}
	next, changed := upsert(string(body), exp.Name, line)
	if changed {
		err = os.WriteFile(path, []byte(next), 0o644)
	}
	return adapter.Change{File: "Gemfile", Entry: exp.Name, Changed: changed}, err
}

func (Adapter) Unwire(_ context.Context, root string, _ adapter.Dependency, exp adapter.Export) (adapter.Change, error) {
	path := filepath.Join(root, "Gemfile")
	body, err := os.ReadFile(path)
	if err != nil {
		return adapter.Change{}, err
	}
	re := dependencyLine(exp.Name)
	if !re.Match(body) {
		return adapter.Change{File: "Gemfile", Entry: exp.Name}, nil
	}
	next := re.ReplaceAllString(string(body), "")
	err = os.WriteFile(path, []byte(next), 0o644)
	return adapter.Change{File: "Gemfile", Entry: exp.Name, Changed: true}, err
}

func (Adapter) Refresh(ctx context.Context, _ string, _ adapter.Dependency, _ adapter.Export, _ adapter.Locked) error {
	return adapter.RequireTool(ctx, "gem", "bundler")
}

func (Adapter) Drift(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	body, err := os.ReadFile(filepath.Join(root, "Gemfile"))
	if err != nil {
		return nil, err
	}
	line := strings.TrimSpace(dependencyLine(exp.Name).FindString(string(body)))
	urlMatch := regexp.MustCompile(`git:\s*["']([^"']+)["']`).FindStringSubmatch(line)
	gotURL := ""
	if len(urlMatch) == 2 {
		gotURL = urlMatch[1]
	}
	badPin := dep.Track != "floating" && !strings.Contains(line, locked.Commit)
	if line == "" || gitx.NormalizeURL(gotURL) != gitx.NormalizeURL(locked.Git) || badPin {
		return []adapter.Finding{{File: "Gemfile", Entry: exp.Name, Want: locked.Commit, Got: line}}, nil
	}
	return nil, nil
}

func upsert(document, name, line string) (string, bool) {
	re := dependencyLine(name)
	if old := re.FindString(document); old != "" {
		if strings.TrimSpace(old) == line {
			return document, false
		}
		return re.ReplaceAllString(document, line+"\n"), true
	}
	separator := "\n"
	if strings.HasSuffix(document, "\n") {
		separator = ""
	}
	return document + separator + line + "\n", true
}

func dependencyLine(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^[ \t]*gem[ \t]+(?:` + regexp.QuoteMeta(strconv.Quote(name)) + `|'` + regexp.QuoteMeta(name) + `')[ \t]*(?:,.*)?(?:\n|$)`)
}
