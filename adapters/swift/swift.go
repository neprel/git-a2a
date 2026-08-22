package swift

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

func (Adapter) Ecosystem() string { return "swift" }
func (Adapter) Detect(root string) (bool, adapter.Variant, error) {
	_, err := os.Stat(filepath.Join(root, "Package.swift"))
	if os.IsNotExist(err) {
		return false, "", nil
	}
	return err == nil, "swiftpm", err
}

func (Adapter) Wire(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	file := filepath.Join(root, "Package.swift")
	body, err := os.ReadFile(file)
	if err != nil {
		return adapter.Change{}, err
	}
	key, value := "revision", locked.Commit
	if dep.Track == "floating" {
		key, value = "branch", dep.Ref
	}
	entry := fmt.Sprintf(".package(url: %s, %s: %s)", strconv.Quote(dep.Git), key, strconv.Quote(value))
	next, changed, err := upsert(string(body), exp.Name, dep.Git, entry)
	if err != nil {
		return adapter.Change{}, adapter.NotWirable(err.Error())
	}
	if changed {
		err = os.WriteFile(file, []byte(next), 0o644)
	}
	return adapter.Change{File: "Package.swift", Entry: exp.Name, Changed: changed}, err
}

func (Adapter) Unwire(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export) (adapter.Change, error) {
	file := filepath.Join(root, "Package.swift")
	body, err := os.ReadFile(file)
	if err != nil {
		return adapter.Change{}, err
	}
	if re := createdDependencies(exp.Name); re.Match(body) {
		next := re.ReplaceAll(body, nil)
		err = os.WriteFile(file, next, 0o644)
		return adapter.Change{File: "Package.swift", Entry: exp.Name, Changed: true}, err
	}
	if re := emptyDependencies(exp.Name); re.Match(body) {
		next := re.ReplaceAll(body, []byte("[]"))
		err = os.WriteFile(file, next, 0o644)
		return adapter.Change{File: "Package.swift", Entry: exp.Name, Changed: true}, err
	}
	if re := managedEntry(exp.Name); re.Match(body) {
		next := re.ReplaceAll(body, nil)
		next = separatorMarker(exp.Name).ReplaceAll(next, nil)
		err = os.WriteFile(file, next, 0o644)
		return adapter.Change{File: "Package.swift", Entry: exp.Name, Changed: true}, err
	}
	next, changed, err := remove(string(body), dep.Git)
	if err != nil {
		return adapter.Change{}, err
	}
	if changed {
		err = os.WriteFile(file, []byte(next), 0o644)
	}
	return adapter.Change{File: "Package.swift", Entry: exp.Name, Changed: changed}, err
}

func (Adapter) Refresh(context.Context, string, adapter.Dependency, adapter.Export, adapter.Locked) error {
	return nil
}

func (Adapter) Drift(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	body, err := os.ReadFile(filepath.Join(root, "Package.swift"))
	if err != nil {
		return nil, err
	}
	entry, _ := findEntry(string(body), locked.Git)
	urlMatch := regexp.MustCompile(`url\s*:\s*"([^"]+)"`).FindStringSubmatch(entry)
	gotURL := ""
	if len(urlMatch) == 2 {
		gotURL = urlMatch[1]
	}
	badURL := gotURL == "" || gitx.NormalizeURL(gotURL) != gitx.NormalizeURL(locked.Git)
	badPin := dep.Track != "floating" && !strings.Contains(entry, locked.Commit)
	if entry == "" || badURL || badPin {
		return []adapter.Finding{{File: "Package.swift", Entry: exp.Name, Want: locked.Commit, Got: strings.TrimSpace(entry)}}, nil
	}
	return nil, nil
}

func upsert(document, name, gitURL, entry string) (string, bool, error) {
	if managedEntry(name).MatchString(document) || emptyDependencies(name).MatchString(document) || createdDependencies(name).MatchString(document) {
		current, _ := findEntry(document, gitURL)
		if current == entry {
			return document, false, nil
		}
	}
	open, close, ok := dependencyArray(document)
	if !ok {
		targets := topLevelArgument(document, "targets")
		if targets < 0 {
			return "", false, fmt.Errorf("Package.swift: top-level Package targets argument is required")
		}
		lineStart := strings.LastIndex(document[:targets], "\n") + 1
		indent := document[lineStart:targets]
		block := indent + "// git-a2a:begin-container " + name + "\n" + indent + "dependencies: [\n" + indent + "    " + entry + ",\n" + indent + "],\n" + indent + "// git-a2a:end-container " + name + "\n"
		return document[:lineStart] + block + document[lineStart:], true, nil
	}
	if _, span := findEntry(document[open+1:close], gitURL); span != nil {
		start, end := open+1+span[0], open+1+span[1]
		if document[start:end] == entry {
			return document, false, nil
		}
		return document[:start] + entry + document[end:], true, nil
	}
	content := document[open+1 : close]
	indent := "        "
	if match := regexp.MustCompile(`(?m)^([ \t]*)\.package\s*\(`).FindStringSubmatch(content); len(match) == 2 {
		indent = match[1]
	}
	if strings.TrimSpace(content) == "" {
		block := "[ // git-a2a:empty " + name + "\n" + indent + entry + ",\n    ]"
		return document[:open] + block + document[close+1:], true, nil
	}
	insertAt := close
	for insertAt > open+1 && (document[insertAt-1] == ' ' || document[insertAt-1] == '\t' || document[insertAt-1] == '\n' || document[insertAt-1] == '\r') {
		insertAt--
	}
	updated := document
	entries := packageEntries(content)
	if len(entries) > 0 {
		lastEnd := open + 1 + entries[len(entries)-1][1]
		cursor := lastEnd
		for cursor < close && (document[cursor] == ' ' || document[cursor] == '\t') {
			cursor++
		}
		if cursor >= close || document[cursor] != ',' {
			updated = document[:lastEnd] + ", // git-a2a:separator " + name + document[lastEnd:]
			delta := len(", // git-a2a:separator " + name)
			insertAt += delta
			close += delta
		}
	}
	block := "\n" + indent + "// git-a2a:begin " + name + "\n" + indent + entry + ",\n" + indent + "// git-a2a:end " + name
	return updated[:insertAt] + block + updated[insertAt:], true, nil
}

func remove(document, gitURL string) (string, bool, error) {
	open, close, ok := dependencyArray(document)
	if !ok {
		return document, false, nil
	}
	_, span := findEntry(document[open+1:close], gitURL)
	if span == nil {
		return document, false, nil
	}
	start, end := open+1+span[0], open+1+span[1]
	lineStart := strings.LastIndex(document[:start], "\n") + 1
	if strings.TrimSpace(document[lineStart:start]) == "" {
		start = lineStart
	}
	for end < close && (document[end] == ' ' || document[end] == '\t') {
		end++
	}
	if end < close && document[end] == ',' {
		end++
	}
	if end < close && document[end] == '\n' {
		end++
	}
	return document[:start] + document[end:], true, nil
}

func dependencyArray(document string) (int, int, bool) {
	dependencyAt := topLevelArgument(document, "dependencies")
	if dependencyAt < 0 {
		return 0, 0, false
	}
	open := strings.Index(document[dependencyAt:], "[")
	if open < 0 {
		return 0, 0, false
	}
	open += dependencyAt
	close := matching(document, open, '[', ']')
	return open, close, close > open
}

func topLevelArgument(document, name string) int {
	packageAt := strings.Index(document, "Package(")
	if packageAt < 0 {
		return -1
	}
	open := packageAt + strings.Index(document[packageAt:], "(")
	paren, bracket, brace := 1, 0, 0
	quoted, escaped, lineComment, blockComment := false, false, false, false
	for i := open + 1; i < len(document); i++ {
		c := document[i]
		if lineComment {
			if c == '\n' {
				lineComment = false
			}
			continue
		}
		if blockComment {
			if c == '*' && i+1 < len(document) && document[i+1] == '/' {
				blockComment = false
				i++
			}
			continue
		}
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '/' && i+1 < len(document) && document[i+1] == '/' {
			lineComment = true
			i++
			continue
		}
		if c == '/' && i+1 < len(document) && document[i+1] == '*' {
			blockComment = true
			i++
			continue
		}
		if c == '"' {
			quoted = true
			continue
		}
		switch c {
		case '(':
			paren++
		case ')':
			paren--
		case '[':
			bracket++
		case ']':
			bracket--
		case '{':
			brace++
		case '}':
			brace--
		}
		if paren == 0 {
			break
		}
		if paren == 1 && bracket == 0 && brace == 0 && strings.HasPrefix(document[i:], name) {
			beforeOK := i == 0 || !(document[i-1] == '_' || document[i-1] >= 'A' && document[i-1] <= 'Z' || document[i-1] >= 'a' && document[i-1] <= 'z')
			j := i + len(name)
			for j < len(document) && (document[j] == ' ' || document[j] == '\t') {
				j++
			}
			if beforeOK && j < len(document) && document[j] == ':' {
				return i
			}
		}
	}
	return -1
}

func managedEntry(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?ms)^[ \t]*// git-a2a:begin ` + regexp.QuoteMeta(name) + `\n.*?^[ \t]*// git-a2a:end ` + regexp.QuoteMeta(name) + `(?:\n|$)`)
}

func separatorMarker(name string) *regexp.Regexp {
	return regexp.MustCompile(`, // git-a2a:separator ` + regexp.QuoteMeta(name))
}

func emptyDependencies(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?ms)\[[ \t]*// git-a2a:empty ` + regexp.QuoteMeta(name) + `\n.*?\]`)
}

func createdDependencies(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?ms)^[ \t]*// git-a2a:begin-container ` + regexp.QuoteMeta(name) + `\n.*?^[ \t]*// git-a2a:end-container ` + regexp.QuoteMeta(name) + `\n`)
}

func findEntry(document, gitURL string) (string, []int) {
	for _, span := range packageEntries(document) {
		entry := document[span[0]:span[1]]
		match := regexp.MustCompile(`url\s*:\s*"([^"]+)"`).FindStringSubmatch(entry)
		if len(match) == 2 && gitx.NormalizeURL(match[1]) == gitx.NormalizeURL(gitURL) {
			return entry, span
		}
	}
	return "", nil
}

func packageEntries(document string) [][]int {
	var entries [][]int
	for from := 0; from < len(document); {
		relative := strings.Index(document[from:], ".package")
		if relative < 0 {
			break
		}
		start := from + relative
		open := strings.Index(document[start:], "(")
		if open < 0 {
			break
		}
		open += start
		end := matching(document, open, '(', ')')
		if end < 0 {
			break
		}
		end++
		entries = append(entries, []int{start, end})
		from = end
	}
	return entries
}

func matching(value string, open int, left, right byte) int {
	depth := 0
	quoted, escaped := false, false
	for index := open; index < len(value); index++ {
		char := value[index]
		if quoted {
			if escaped {
				escaped = false
			} else if char == '\\' {
				escaped = true
			} else if char == '"' {
				quoted = false
			}
			continue
		}
		if char == '"' {
			quoted = true
		} else if char == left {
			depth++
		} else if char == right {
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}
