package composer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/neprel/git-a2a/internal/adapter"
	"github.com/neprel/git-a2a/internal/gitx"
)

type Adapter struct{}

func (Adapter) Ecosystem() string { return "composer" }

func (Adapter) Detect(root string) (bool, adapter.Variant, error) {
	_, err := os.Stat(filepath.Join(root, "composer.json"))
	if os.IsNotExist(err) {
		return false, "", nil
	}
	return err == nil, "composer", err
}

func (Adapter) Wire(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	if exp.Path != "" && exp.Path != "." {
		return adapter.Change{}, adapter.NotWirable("Composer VCS repositories require composer.json at the repository root")
	}
	path := filepath.Join(root, "composer.json")
	body, err := os.ReadFile(path)
	if err != nil {
		return adapter.Change{}, err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil {
		return adapter.Change{}, err
	}
	if raw := doc["repositories"]; len(raw) > 0 && strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
		return adapter.Change{}, adapter.NotWirable("composer.json repositories must use the named object form for addressable wiring")
	}
	repository, _ := json.Marshal(map[string]string{"type": "vcs", "url": dep.Git})
	next, repositoryChanged, err := setObjectEntry(body, "repositories", exp.Name, repository)
	if err != nil {
		return adapter.Change{}, err
	}
	constraint := "dev-" + branchName(dep.Ref)
	if dep.Track != "floating" {
		constraint += "#" + locked.Commit
	}
	next, requireChanged, err := setObjectEntry(next, "require", exp.Name, mustJSON(constraint))
	if err == nil && (repositoryChanged || requireChanged) {
		err = os.WriteFile(path, next, 0o644)
	}
	return adapter.Change{File: "composer.json", Entry: "require." + exp.Name, Changed: repositoryChanged || requireChanged}, err
}

func (Adapter) Unwire(_ context.Context, root string, _ adapter.Dependency, exp adapter.Export) (adapter.Change, error) {
	path := filepath.Join(root, "composer.json")
	body, err := os.ReadFile(path)
	if err != nil {
		return adapter.Change{}, err
	}
	next, requireChanged, err := removeObjectEntry(body, "require", exp.Name)
	if err != nil {
		return adapter.Change{}, err
	}
	next, repositoryChanged, err := removeObjectEntry(next, "repositories", exp.Name)
	if repositoryChanged {
		next = removeEmptyObject(next, "repositories")
	}
	if err == nil && (requireChanged || repositoryChanged) {
		err = os.WriteFile(path, next, 0o644)
	}
	return adapter.Change{File: "composer.json", Entry: "require." + exp.Name, Changed: requireChanged || repositoryChanged}, err
}

func (Adapter) Refresh(context.Context, string, adapter.Dependency, adapter.Export, adapter.Locked) error {
	return nil
}

func (Adapter) Drift(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	body, err := os.ReadFile(filepath.Join(root, "composer.json"))
	if err != nil {
		return nil, err
	}
	var doc struct {
		Repositories map[string]struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"repositories"`
		Require map[string]string `json:"require"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	repository := doc.Repositories[exp.Name]
	constraint := doc.Require[exp.Name]
	badPin := dep.Track != "floating" && !strings.Contains(constraint, locked.Commit)
	if repository.Type != "vcs" || gitx.NormalizeURL(repository.URL) != gitx.NormalizeURL(locked.Git) || constraint == "" || badPin {
		return []adapter.Finding{{File: "composer.json", Entry: exp.Name, Want: locked.Commit, Got: repository.URL + " " + constraint}}, nil
	}
	return nil, nil
}

func branchName(ref string) string {
	ref = strings.TrimPrefix(ref, "refs/heads/")
	if ref == "" || regexp.MustCompile(`^[0-9a-fA-F]{40}$`).MatchString(ref) {
		return "main"
	}
	return ref
}

func mustJSON(value string) []byte {
	body, _ := json.Marshal(value)
	return body
}

func setObjectEntry(body []byte, object, key string, value []byte) ([]byte, bool, error) {
	start, end, ok := objectRange(body, object)
	entryKey, _ := json.Marshal(key)
	if ok {
		content := string(body[start+1 : end])
		re := regexp.MustCompile(regexp.QuoteMeta(string(entryKey)) + `\s*:`)
		if loc := re.FindStringIndex(content); loc != nil {
			valueStart := loc[1]
			for valueStart < len(content) && strings.ContainsRune(" \t\r\n", rune(content[valueStart])) {
				valueStart++
			}
			valueEnd, scanErr := jsonValueEnd(content, valueStart)
			if scanErr != nil {
				return nil, false, scanErr
			}
			if string(value) == content[valueStart:valueEnd] {
				return body, false, nil
			}
			content = content[:valueStart] + string(value) + content[valueEnd:]
			return []byte(string(body[:start+1]) + content + string(body[end:])), true, nil
		}
		trimmed := strings.TrimRight(content, " \t\r\n")
		suffix := content[len(trimmed):]
		if strings.TrimSpace(trimmed) != "" {
			trimmed += ","
		}
		trimmed += "\n    " + string(entryKey) + ": " + string(value)
		return []byte(string(body[:start+1]) + trimmed + suffix + string(body[end:])), true, nil
	}
	last := strings.LastIndex(string(body), "}")
	if last < 0 {
		return nil, false, fmt.Errorf("composer.json: no top-level object")
	}
	prefix := strings.TrimRight(string(body[:last]), " \t\r\n")
	comma := ""
	if !strings.HasSuffix(prefix, "{") {
		comma = ","
	}
	next := prefix + comma + "\n  " + strconvQuote(object) + ": {\n    " + string(entryKey) + ": " + string(value) + "\n  }\n" + string(body[last:])
	return []byte(next), true, nil
}

func removeObjectEntry(body []byte, object, key string) ([]byte, bool, error) {
	start, end, ok := objectRange(body, object)
	if !ok {
		return body, false, nil
	}
	content := string(body[start+1 : end])
	entryKey, _ := json.Marshal(key)
	re := regexp.MustCompile(regexp.QuoteMeta(string(entryKey)) + `\s*:`)
	loc := re.FindStringIndex(content)
	if loc == nil {
		return body, false, nil
	}
	valueStart := loc[1]
	for valueStart < len(content) && strings.ContainsRune(" \t\r\n", rune(content[valueStart])) {
		valueStart++
	}
	valueEnd, err := jsonValueEnd(content, valueStart)
	if err != nil {
		return nil, false, err
	}
	removeStart, removeEnd := loc[0], valueEnd
	for removeEnd < len(content) && strings.ContainsRune(" \t\r\n", rune(content[removeEnd])) {
		removeEnd++
	}
	if removeEnd < len(content) && content[removeEnd] == ',' {
		removeEnd++
	} else {
		for removeStart > 0 && strings.ContainsRune(" \t\r\n", rune(content[removeStart-1])) {
			removeStart--
		}
		if removeStart > 0 && content[removeStart-1] == ',' {
			removeStart--
		}
	}
	content = content[:removeStart] + content[removeEnd:]
	return []byte(string(body[:start+1]) + content + string(body[end:])), true, nil
}

func objectRange(body []byte, key string) (int, int, bool) {
	encoded, _ := json.Marshal(key)
	re := regexp.MustCompile(regexp.QuoteMeta(string(encoded)) + `\s*:\s*\{`)
	loc := re.FindIndex(body)
	if loc == nil {
		return 0, 0, false
	}
	start := loc[1] - 1
	end, err := jsonValueEnd(string(body), start)
	return start, end - 1, err == nil
}

func removeEmptyObject(body []byte, key string) []byte {
	start, end, ok := objectRange(body, key)
	if !ok || strings.TrimSpace(string(body[start+1:end])) != "" {
		return body
	}
	encoded, _ := json.Marshal(key)
	keyAt := bytes.LastIndex(body[:start], encoded)
	if keyAt < 0 {
		return body
	}
	removeStart, removeEnd := keyAt, end+1
	for removeStart > 0 && (body[removeStart-1] == ' ' || body[removeStart-1] == '\t') {
		removeStart--
	}
	for removeEnd < len(body) && strings.ContainsRune(" \t\r\n", rune(body[removeEnd])) {
		removeEnd++
	}
	if removeEnd < len(body) && body[removeEnd] == ',' {
		removeEnd++
	} else {
		for removeStart > 0 && strings.ContainsRune(" \t\r\n", rune(body[removeStart-1])) {
			removeStart--
		}
		if removeStart > 0 && body[removeStart-1] == ',' {
			removeStart--
		}
	}
	return append(append([]byte(nil), body[:removeStart]...), body[removeEnd:]...)
}

func jsonValueEnd(document string, start int) (int, error) {
	if start >= len(document) {
		return 0, fmt.Errorf("composer.json: missing value")
	}
	if document[start] == '"' {
		escape := false
		for i := start + 1; i < len(document); i++ {
			if escape {
				escape = false
			} else if document[i] == '\\' {
				escape = true
			} else if document[i] == '"' {
				return i + 1, nil
			}
		}
	}
	if document[start] == '{' || document[start] == '[' {
		open, close := document[start], byte('}')
		if open == '[' {
			close = ']'
		}
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
			} else if c == open {
				depth++
			} else if c == close {
				depth--
				if depth == 0 {
					return i + 1, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("composer.json: unsupported or unterminated value")
}

func strconvQuote(value string) string {
	body, _ := json.Marshal(value)
	return string(body)
}
