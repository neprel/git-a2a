package hex

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

func (Adapter) Ecosystem() string { return "hex" }

func (Adapter) Detect(root string) (bool, adapter.Variant, error) {
	_, err := os.Stat(filepath.Join(root, "mix.exs"))
	if os.IsNotExist(err) {
		return false, "", nil
	}
	return err == nil, "mix", err
}

func (Adapter) Wire(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	path := filepath.Join(root, "mix.exs")
	body, err := os.ReadFile(path)
	if err != nil {
		return adapter.Change{}, err
	}
	pinKey, pin := "ref", locked.Commit
	if dep.Track == "floating" {
		pinKey, pin = "branch", dep.Ref
	}
	line := fmt.Sprintf("    {%s, git: %s, %s: %s", atom(exp.Name), strconv.Quote(dep.Git), pinKey, strconv.Quote(pin))
	if exp.Path != "" && exp.Path != "." {
		line += ", sparse: " + strconv.Quote(exp.Path)
	}
	line += "}"
	next, changed, err := upsert(string(body), exp.Name, line)
	if err == nil && changed {
		err = os.WriteFile(path, []byte(next), 0o644)
	}
	return adapter.Change{File: "mix.exs", Entry: exp.Name, Changed: changed}, err
}

func (Adapter) Unwire(_ context.Context, root string, _ adapter.Dependency, exp adapter.Export) (adapter.Change, error) {
	path := filepath.Join(root, "mix.exs")
	body, err := os.ReadFile(path)
	if err != nil {
		return adapter.Change{}, err
	}
	if re := managedBlock(exp.Name); re.Match(body) {
		next := re.ReplaceAll(body, nil)
		next = separatorMarker(exp.Name).ReplaceAll(next, nil)
		err = os.WriteFile(path, next, 0o644)
		return adapter.Change{File: "mix.exs", Entry: exp.Name, Changed: true}, err
	}
	re := dependencyLine(exp.Name)
	if !re.Match(body) {
		return adapter.Change{File: "mix.exs", Entry: exp.Name}, nil
	}
	next := re.ReplaceAllString(string(body), "")
	err = os.WriteFile(path, []byte(next), 0o644)
	return adapter.Change{File: "mix.exs", Entry: exp.Name, Changed: true}, err
}

func (Adapter) Refresh(context.Context, string, adapter.Dependency, adapter.Export, adapter.Locked) error {
	return nil
}

func (Adapter) Drift(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	body, err := os.ReadFile(filepath.Join(root, "mix.exs"))
	if err != nil {
		return nil, err
	}
	line := strings.TrimSpace(dependencyLine(exp.Name).FindString(string(body)))
	match := regexp.MustCompile(`git:\s*["']([^"']+)["']`).FindStringSubmatch(line)
	gotURL := ""
	if len(match) == 2 {
		gotURL = match[1]
	}
	badPin := dep.Track != "floating" && !strings.Contains(line, locked.Commit)
	if line == "" || gitx.NormalizeURL(gotURL) != gitx.NormalizeURL(locked.Git) || badPin {
		return []adapter.Finding{{File: "mix.exs", Entry: exp.Name, Want: locked.Commit, Got: line}}, nil
	}
	return nil, nil
}

func upsert(document, name, line string) (string, bool, error) {
	if block := managedBlock(name).FindString(document); block != "" {
		if strings.Contains(block, line) {
			return document, false, nil
		}
		old := dependencyLine(name).FindString(block)
		if old == "" {
			return "", false, fmt.Errorf("mix.exs: malformed git-a2a dependency block for %s", name)
		}
		replacement := strings.Replace(block, old, line+",\n", 1)
		return strings.Replace(document, block, replacement, 1), true, nil
	}
	re := dependencyLine(name)
	if old := re.FindString(document); old != "" {
		if strings.TrimSpace(old) == strings.TrimSpace(line) {
			return document, false, nil
		}
		return re.ReplaceAllString(document, line+",\n"), true, nil
	}
	start, end, ok := depsList(document)
	if !ok {
		return "", false, fmt.Errorf("mix.exs: deps list is required")
	}
	content := document[start+1 : end]
	trimmed := strings.TrimRight(content, " \t\r\n")
	suffix := content[len(trimmed):]
	if strings.TrimSpace(trimmed) != "" && !strings.HasSuffix(strings.TrimSpace(trimmed), ",") {
		trimmed += ", # git-a2a:separator " + name
	}
	trimmed += "\n    # git-a2a:begin " + name + "\n" + line + ",\n    # git-a2a:end " + name
	return document[:start+1] + trimmed + suffix + document[end:], true, nil
}

func depsList(document string) (int, int, bool) {
	anchors := []*regexp.Regexp{
		regexp.MustCompile(`(?m)^[ \t]*defp?[ \t]+deps[ \t]+do\b`),
		regexp.MustCompile(`(?m)\bdeps:[ \t]*`),
	}
	for _, anchor := range anchors {
		loc := anchor.FindStringIndex(document)
		if loc == nil {
			continue
		}
		start := strings.Index(document[loc[1]:], "[")
		if start < 0 {
			continue
		}
		start += loc[1]
		if end, ok := matchingBracket(document, start); ok {
			return start, end, true
		}
	}
	return 0, 0, false
}

func matchingBracket(document string, start int) (int, bool) {
	depth, inString, escape := 0, false, false
	for i := start; i < len(document); i++ {
		c := document[i]
		if inString {
			if escape {
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
		} else if c == '[' {
			depth++
		} else if c == ']' {
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

func atom(name string) string {
	if regexp.MustCompile(`^[a-z_][a-zA-Z0-9_]*[!?]?$`).MatchString(name) {
		return ":" + name
	}
	return ":" + strconv.Quote(name)
}

func atomPattern(name string) string {
	patterns := []string{regexp.QuoteMeta(":" + strconv.Quote(name))}
	if regexp.MustCompile(`^[a-z_][a-zA-Z0-9_]*[!?]?$`).MatchString(name) {
		patterns = append(patterns, regexp.QuoteMeta(":"+name))
	}
	return `(?:` + strings.Join(patterns, `|`) + `)`
}

func dependencyLine(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^[ \t]*\{[ \t]*` + atomPattern(name) + `[ \t]*,.*\}[ \t]*,?[ \t]*(?:\n|$)`)
}

func managedBlock(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?ms)\n[ \t]*# git-a2a:begin ` + regexp.QuoteMeta(name) + `\n.*?^[ \t]*# git-a2a:end ` + regexp.QuoteMeta(name))
}

func separatorMarker(name string) *regexp.Regexp {
	return regexp.MustCompile(`, # git-a2a:separator ` + regexp.QuoteMeta(name))
}
