package clojure

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

func (Adapter) Ecosystem() string { return "clojure" }

func (Adapter) Detect(root string) (bool, adapter.Variant, error) {
	_, err := os.Stat(filepath.Join(root, "deps.edn"))
	if os.IsNotExist(err) {
		return false, "", nil
	}
	return err == nil, "tools-deps", err
}

func (Adapter) Wire(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	if !validLib(exp.Name) {
		return adapter.Change{}, adapter.NotWirable("Clojure dependency names must be qualified lib symbols")
	}
	path := filepath.Join(root, "deps.edn")
	body, err := os.ReadFile(path)
	if err != nil {
		return adapter.Change{}, err
	}
	entry := exp.Name + " {:git/url " + strconv.Quote(dep.Git) + " :git/sha " + strconv.Quote(locked.Commit)
	if exp.Path != "" && exp.Path != "." {
		entry += " :deps/root " + strconv.Quote(exp.Path)
	}
	entry += "}"
	next, changed, err := upsert(string(body), exp.Name, entry)
	if err == nil && changed {
		err = os.WriteFile(path, []byte(next), 0o644)
	}
	return adapter.Change{File: "deps.edn", Entry: exp.Name, Changed: changed}, err
}

func (Adapter) Unwire(_ context.Context, root string, _ adapter.Dependency, exp adapter.Export) (adapter.Change, error) {
	path := filepath.Join(root, "deps.edn")
	body, err := os.ReadFile(path)
	if err != nil {
		return adapter.Change{}, err
	}
	re := managedBlock(exp.Name)
	if !re.Match(body) {
		return adapter.Change{File: "deps.edn", Entry: exp.Name}, nil
	}
	next := re.ReplaceAllString(string(body), "")
	err = os.WriteFile(path, []byte(next), 0o644)
	return adapter.Change{File: "deps.edn", Entry: exp.Name, Changed: true}, err
}

func (Adapter) Refresh(context.Context, string, adapter.Dependency, adapter.Export, adapter.Locked) error {
	return nil
}

func (Adapter) Drift(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	body, err := os.ReadFile(filepath.Join(root, "deps.edn"))
	if err != nil {
		return nil, err
	}
	block := managedBlock(exp.Name).FindString(string(body))
	match := regexp.MustCompile(`:git/url\s+"([^"]+)"`).FindStringSubmatch(block)
	gotURL := ""
	if len(match) == 2 {
		gotURL = match[1]
	}
	if block == "" || gitx.NormalizeURL(gotURL) != gitx.NormalizeURL(locked.Git) || !strings.Contains(block, locked.Commit) {
		return []adapter.Finding{{File: "deps.edn", Entry: exp.Name, Want: locked.Commit, Got: strings.TrimSpace(block)}}, nil
	}
	return nil, nil
}

func upsert(document, name, entry string) (string, bool, error) {
	start, end, ok := depsMap(document)
	if !ok {
		return "", false, fmt.Errorf("deps.edn: top-level :deps map is required")
	}
	lineStart := strings.LastIndex(document[:end], "\n") + 1
	closingIndent := document[lineStart:end]
	if strings.TrimSpace(closingIndent) != "" {
		closingIndent = " "
		lineStart = end
	}
	indent := closingIndent + "  "
	block := indent + ";; git-a2a:begin " + name + "\n" + indent + entry + "\n" + indent + ";; git-a2a:end " + name + "\n"
	re := managedBlock(name)
	if old := re.FindString(document); old != "" {
		if old == block {
			return document, false, nil
		}
		return re.ReplaceAllStringFunc(document, func(string) string { return block }), true, nil
	}
	prefix := ""
	if lineStart == end {
		prefix = "\n"
	}
	_ = start
	return document[:lineStart] + prefix + block + document[lineStart:], true, nil
}

func depsMap(document string) (int, int, bool) {
	loc := regexp.MustCompile(`(?m):deps\s*\{`).FindStringIndex(document)
	if loc == nil {
		return 0, 0, false
	}
	start := strings.LastIndex(document[:loc[1]], "{")
	if start < 0 {
		return 0, 0, false
	}
	end, ok := matchingBrace(document, start)
	return start, end, ok
}

func matchingBrace(document string, start int) (int, bool) {
	depth, inString, escape, comment := 0, false, false, false
	for i := start; i < len(document); i++ {
		c := document[i]
		if comment {
			if c == '\n' {
				comment = false
			}
			continue
		}
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
		if c == ';' {
			comment = true
		} else if c == '"' {
			inString = true
		} else if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

func managedBlock(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?ms)^[ \t]*;; git-a2a:begin ` + regexp.QuoteMeta(name) + `\n.*?^[ \t]*;; git-a2a:end ` + regexp.QuoteMeta(name) + `\n`)
}

func validLib(name string) bool {
	return regexp.MustCompile(`^[A-Za-z0-9*+!_?<>.=-]+/[A-Za-z0-9*+!_?<>.=-]+$`).MatchString(name)
}
