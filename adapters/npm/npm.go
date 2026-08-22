package npm

import (
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

func (Adapter) Ecosystem() string { return "npm" }

func (Adapter) Detect(root string) (bool, adapter.Variant, error) {
	b, err := os.ReadFile(filepath.Join(root, "package.json"))
	if os.IsNotExist(err) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	var p struct {
		PackageManager string `json:"packageManager"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return false, "", err
	}
	if _, err := os.Stat(filepath.Join(root, ".yarnrc.yml")); err == nil {
		return true, "yarn-berry", nil
	}
	if strings.HasPrefix(p.PackageManager, "yarn@") {
		return true, "yarn-berry", nil
	}
	if _, err := os.Stat(filepath.Join(root, "pnpm-lock.yaml")); err == nil {
		return true, "pnpm", nil
	}
	if _, err := os.Stat(filepath.Join(root, "bun.lock")); err == nil {
		return true, "bun", nil
	}
	if _, err := os.Stat(filepath.Join(root, "bun.lockb")); err == nil {
		return true, "bun", nil
	}
	return true, "npm", nil
}

func (a Adapter) Wire(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	ok, v, err := a.Detect(root)
	if err != nil || !ok {
		return adapter.Change{}, err
	}
	if exp.Path != "" && exp.Path != "." && v != "pnpm" && v != "yarn-berry" {
		return adapter.Change{}, adapter.NotWirable(fmt.Sprintf("%s cannot express subdirectory %s", v, exp.Path))
	}
	if exp.Path != "" && exp.Path != "." && (v == "npm" || v == "bun") {
		return adapter.Change{}, fmt.Errorf("npm export %s has subdirectory %s: %s does not support git subdirectory dependencies", exp.Name, exp.Path, v)
	}
	pin := dependencyURL(dep, locked, string(v), exp.Path)
	p := filepath.Join(root, "package.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return adapter.Change{}, err
	}
	next, changed, err := setDependency(b, exp.Name, pin)
	if err == nil && changed {
		err = os.WriteFile(p, next, 0o644)
	}
	return adapter.Change{File: "package.json", Entry: "dependencies." + exp.Name, Changed: changed}, err
}

func (a Adapter) Unwire(_ context.Context, root string, _ adapter.Dependency, exp adapter.Export) (adapter.Change, error) {
	p := filepath.Join(root, "package.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return adapter.Change{}, err
	}
	next, changed, err := removeDependency(b, exp.Name)
	if err == nil && changed {
		err = os.WriteFile(p, next, 0o644)
	}
	return adapter.Change{File: "package.json", Entry: "dependencies." + exp.Name, Changed: changed}, err
}

func (a Adapter) Refresh(ctx context.Context, root string, _ adapter.Dependency, exp adapter.Export, _ adapter.Locked) error {
	_, v, err := a.Detect(root)
	if err != nil {
		return err
	}
	if v == "npm" {
		if _, err := os.Stat(filepath.Join(root, "package-lock.json")); os.IsNotExist(err) {
			return nil
		}
	}
	command := refreshCommand(v, exp.Name)
	return adapter.Command(ctx, root, command[0], command[1:]...)
}

func refreshCommand(variant adapter.Variant, name string) []string {
	switch variant {
	case "yarn-berry":
		return []string{"yarn", "up", name}
	case "pnpm":
		return []string{"pnpm", "update", name}
	case "bun":
		return []string{"bun", "update", name}
	default:
		return []string{"npm", "install", "--package-lock-only", "--ignore-scripts", "--no-audit", "--no-fund"}
	}
}

func (a Adapter) Drift(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	b, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return nil, err
	}
	var p struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	got := p.Dependencies[exp.Name]
	base := got
	if i := strings.Index(base, "#"); i >= 0 {
		base = base[:i]
	}
	badURL := got == "" || gitx.NormalizeURL(base) != gitx.NormalizeURL(locked.Git)
	badPin := dep.Track != "floating" && !strings.Contains(got, locked.Commit)
	if badURL || badPin {
		return []adapter.Finding{{File: "package.json", Entry: exp.Name, Want: locked.Commit, Got: got}}, nil
	}
	return nil, nil
}

func dependencyURL(dep adapter.Dependency, locked adapter.Locked, variant, path string) string {
	ref := locked.Commit
	if dep.Track == "floating" {
		ref = dep.Ref
	}
	url := dep.Git
	if strings.HasPrefix(url, "git@") {
		parts := strings.SplitN(url, ":", 2)
		if len(parts) == 2 {
			url = "ssh://" + parts[0] + "/" + parts[1]
		}
	}
	if !strings.HasPrefix(url, "git+") {
		url = "git+" + url
	}
	if variant == "yarn-berry" {
		if dep.Track == "floating" {
			url += "#head=" + ref
		} else {
			url += "#commit=" + ref
		}
		if path != "" && path != "." {
			url += "&workspace=" + path
		}
		return url
	}
	url += "#" + ref
	if path != "" && path != "." && variant == "pnpm" {
		url += "&path:/" + strings.TrimPrefix(path, "/")
	}
	return url
}

func setDependency(b []byte, name, value string) ([]byte, bool, error) {
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, false, err
	}
	deps, _ := doc["dependencies"].(map[string]any)
	if deps != nil {
		if old, _ := deps[name].(string); old == value {
			return b, false, nil
		}
	}
	start, end, ok := objectRange(b, "dependencies")
	entry := fmt.Sprintf("    %q: %q", name, value)
	if ok {
		body := string(b[start+1 : end])
		pattern := regexp.MustCompile(`(` + regexp.QuoteMeta(fmt.Sprintf("%q", name)) + `\s*:\s*)"[^"]*"`)
		if pattern.MatchString(body) {
			encoded, _ := json.Marshal(value)
			body = pattern.ReplaceAllString(body, "${1}"+string(encoded))
			return []byte(string(b[:start+1]) + body + string(b[end:])), true, nil
		}
		trim := strings.TrimRight(body, " \t\r\n")
		suffix := body[len(trim):]
		if strings.TrimSpace(trim) != "" {
			trim += ","
		}
		trim += "\n" + entry
		return []byte(string(b[:start+1]) + trim + suffix + string(b[end:])), true, nil
	}
	last := strings.LastIndex(string(b), "}")
	if last < 0 {
		return nil, false, fmt.Errorf("package.json: no top-level object")
	}
	prefix := strings.TrimRight(string(b[:last]), " \t\r\n")
	comma := ""
	if !strings.HasSuffix(prefix, "{") {
		comma = ","
	}
	next := prefix + comma + "\n  \"dependencies\": {\n" + entry + "\n  }\n" + string(b[last:])
	return []byte(next), true, nil
}

func removeDependency(b []byte, name string) ([]byte, bool, error) {
	start, end, ok := objectRange(b, "dependencies")
	if !ok {
		return b, false, nil
	}
	body := string(b[start+1 : end])
	pattern := regexp.MustCompile(regexp.QuoteMeta(fmt.Sprintf("%q", name)) + `\s*:\s*"[^"]*"`)
	loc := pattern.FindStringIndex(body)
	if loc == nil {
		return b, false, nil
	}
	removeStart, removeEnd := loc[0], loc[1]
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
	body = body[:removeStart] + body[removeEnd:]
	return []byte(string(b[:start+1]) + body + string(b[end:])), true, nil
}

func objectRange(b []byte, key string) (int, int, bool) {
	re := regexp.MustCompile(regexp.QuoteMeta(fmt.Sprintf("%q", key)) + `\s*:\s*\{`)
	loc := re.FindIndex(b)
	if loc == nil {
		return 0, 0, false
	}
	start := loc[1] - 1
	depth := 0
	inString := false
	escape := false
	for i := start; i < len(b); i++ {
		c := b[i]
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
		} else if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return start, i, true
			}
		}
	}
	return 0, 0, false
}
