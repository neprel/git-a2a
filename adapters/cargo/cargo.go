package cargo

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

func (Adapter) Ecosystem() string { return "cargo" }

func (Adapter) Detect(root string) (bool, adapter.Variant, error) {
	_, err := os.Stat(filepath.Join(root, "Cargo.toml"))
	if os.IsNotExist(err) {
		return false, "", nil
	}
	return err == nil, "cargo", err
}

func (Adapter) Wire(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	file := filepath.Join(root, "Cargo.toml")
	body, err := os.ReadFile(file)
	if err != nil {
		return adapter.Change{}, err
	}
	pinKey, pinValue := "rev", locked.Commit
	if dep.Track == "floating" {
		pinKey, pinValue = "branch", dep.Ref
	}
	line := fmt.Sprintf("%s = { git = %q, %s = %q }", tomlKey(exp.Name), dep.Git, pinKey, pinValue)
	next, changed := upsert(string(body), exp.Name, line)
	if changed {
		err = os.WriteFile(file, []byte(next), 0o644)
	}
	return adapter.Change{File: "Cargo.toml", Entry: exp.Name, Changed: changed}, err
}

func (Adapter) Unwire(_ context.Context, root string, _ adapter.Dependency, exp adapter.Export) (adapter.Change, error) {
	file := filepath.Join(root, "Cargo.toml")
	body, err := os.ReadFile(file)
	if err != nil {
		return adapter.Change{}, err
	}
	if re := createdDependencies(exp.Name); re.Match(body) {
		next := re.ReplaceAll(body, nil)
		err = os.WriteFile(file, next, 0o644)
		return adapter.Change{File: "Cargo.toml", Entry: exp.Name, Changed: true}, err
	}
	start, end, ok := dependenciesSection(string(body))
	if !ok {
		return adapter.Change{File: "Cargo.toml", Entry: exp.Name}, nil
	}
	section := string(body)[start:end]
	re := dependencyLine(exp.Name)
	if !re.MatchString(section) {
		return adapter.Change{File: "Cargo.toml", Entry: exp.Name}, nil
	}
	section = re.ReplaceAllString(section, "")
	next := string(body[:start]) + section + string(body[end:])
	err = os.WriteFile(file, []byte(next), 0o644)
	return adapter.Change{File: "Cargo.toml", Entry: exp.Name, Changed: true}, err
}

func (Adapter) Refresh(context.Context, string, adapter.Dependency, adapter.Export, adapter.Locked) error {
	return nil
}

func (Adapter) Drift(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	body, err := os.ReadFile(filepath.Join(root, "Cargo.toml"))
	if err != nil {
		return nil, err
	}
	start, end, ok := dependenciesSection(string(body))
	line := ""
	if ok {
		line = strings.TrimSpace(dependencyLine(exp.Name).FindString(string(body)[start:end]))
	}
	urlMatch := regexp.MustCompile(`git[ \t]*=[ \t]*["']([^"']+)["']`).FindStringSubmatch(line)
	gotURL := ""
	if len(urlMatch) == 2 {
		gotURL = urlMatch[1]
	}
	badURL := gotURL == "" || gitx.NormalizeURL(gotURL) != gitx.NormalizeURL(locked.Git)
	badPin := dep.Track != "floating" && !strings.Contains(line, locked.Commit)
	if line == "" || badURL || badPin {
		return []adapter.Finding{{File: "Cargo.toml", Entry: exp.Name, Want: locked.Commit, Got: line}}, nil
	}
	return nil, nil
}

func upsert(document, name, line string) (string, bool) {
	start, end, ok := dependenciesSection(document)
	if !ok {
		separator := "\n"
		if strings.HasSuffix(document, "\n") {
			separator = ""
		}
		block := "# git-a2a:begin " + name + "\n[dependencies]\n" + line + "\n# git-a2a:end " + name + "\n"
		return document + separator + block, true
	}
	section := document[start:end]
	re := dependencyLine(name)
	if old := re.FindString(section); old != "" {
		if strings.TrimSpace(old) == line {
			return document, false
		}
		section = re.ReplaceAllString(section, line+"\n")
	} else {
		if !strings.HasSuffix(section, "\n") {
			section += "\n"
		}
		section += line + "\n"
	}
	return document[:start] + section + document[end:], true
}

func createdDependencies(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?ms)^# git-a2a:begin ` + regexp.QuoteMeta(name) + `\n\[dependencies\]\n.*?^# git-a2a:end ` + regexp.QuoteMeta(name) + `\n`)
}

func dependenciesSection(document string) (int, int, bool) {
	header := regexp.MustCompile(`(?m)^[ \t]*\[dependencies\][ \t]*(?:#.*)?$`).FindStringIndex(document)
	if header == nil {
		return 0, 0, false
	}
	start := header[1]
	if start < len(document) && document[start] == '\n' {
		start++
	}
	end := len(document)
	if next := regexp.MustCompile(`(?m)^[ \t]*\[[^\]]+\]`).FindStringIndex(document[start:]); next != nil {
		end = start + next[0]
	}
	return start, end, true
}

func dependencyLine(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^[ \t]*` + tomlKeyPattern(name) + `[ \t]*=.*(?:\n|$)`)
}

func tomlKey(name string) string {
	if regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(name) {
		return name
	}
	return strconv.Quote(name)
}

func tomlKeyPattern(name string) string {
	patterns := []string{regexp.QuoteMeta(strconv.Quote(name)), regexp.QuoteMeta("'" + name + "'")}
	if regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(name) {
		patterns = append(patterns, regexp.QuoteMeta(name))
	}
	return `(?:` + strings.Join(patterns, `|`) + `)`
}
