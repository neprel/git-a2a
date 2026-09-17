package npm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/neprel/git-a2a/v2/internal/adapter"
)

type Adapter struct{}

func (Adapter) Ecosystem() string { return "npm" }

func (a Adapter) Capability(root string, _ adapter.Dependency, exp adapter.Export) error {
	ok, variant, err := a.Detect(root)
	if err != nil {
		return err
	}
	if !ok {
		return adapter.NotWirable("npm consumer manifest is absent")
	}
	if exp.Path != "" && exp.Path != "." && variant != "pnpm" && variant != "yarn-berry" {
		return adapter.NotWirable(fmt.Sprintf("%s cannot express subdirectory %s", variant, exp.Path))
	}
	return nil
}

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
	switch {
	case strings.HasPrefix(p.PackageManager, "yarn@"):
		return true, "yarn-berry", nil
	case strings.HasPrefix(p.PackageManager, "pnpm@"):
		return true, "pnpm", nil
	case strings.HasPrefix(p.PackageManager, "bun@"):
		return true, "bun", nil
	case strings.HasPrefix(p.PackageManager, "npm@"):
		return true, "npm", nil
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
	pin := dependencyURL(locked, string(v), exp.Path)
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

func (a Adapter) syncNative(ctx context.Context, root string, exp adapter.Export) error {
	_, v, err := a.Detect(root)
	if err != nil {
		return err
	}
	if err := adapter.RequireTool(ctx, a.Ecosystem(), v); err != nil {
		return err
	}
	command := refreshCommand(v, exp.Name)
	return adapter.Command(ctx, root, command[0], command[1:]...)
}

func refreshCommand(variant adapter.Variant, name string) []string {
	switch variant {
	case "yarn-berry":
		return []string{"yarn", "install", "--mode=skip-build"}
	case "pnpm":
		return []string{"pnpm", "install", "--ignore-scripts"}
	case "bun":
		return []string{"bun", "install", "--ignore-scripts"}
	default:
		return []string{"npm", "install", "--ignore-scripts", "--no-audit", "--no-fund"}
	}
}

func (a Adapter) Pull(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	change, err := a.Wire(ctx, root, dep, exp, locked)
	if err != nil {
		return change, err
	}
	if change.Changed {
		_, variant, detectErr := a.Detect(root)
		if detectErr != nil {
			return change, detectErr
		}
		if variant == "yarn-berry" {
			return change, a.syncNative(ctx, root, exp)
		}
		if err = adapter.RequireTool(ctx, a.Ecosystem(), variant); err != nil {
			return change, err
		}
		command := updateCommand(variant, exp.Name)
		return change, adapter.Command(ctx, root, command[0], command[1:]...)
	}
	return change, a.syncNative(ctx, root, exp)
}

func updateCommand(variant adapter.Variant, name string) []string {
	switch variant {
	case "pnpm":
		return []string{"pnpm", "update", name, "--ignore-scripts"}
	case "bun":
		return []string{"bun", "update", name, "--ignore-scripts"}
	default:
		return []string{"npm", "update", name, "--ignore-scripts", "--no-audit", "--no-fund"}
	}
}

func (a Adapter) Remove(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	_, variant, err := a.Detect(root)
	if err != nil {
		return adapter.Change{}, err
	}
	if variant == "bun" {
		if err = adapter.RequireTool(ctx, a.Ecosystem(), variant); err != nil {
			return adapter.Change{}, err
		}
		// Bun's install currently leaves an extraneous Git package in
		// node_modules after its declaration disappears. Let Bun uninstall it
		// while the root entry is still visible, but keep package.json ownership
		// in the byte-preserving editor below by restoring the original bytes
		// before applying Unwire.
		manifestPath := filepath.Join(root, "package.json")
		original, readErr := os.ReadFile(manifestPath)
		if readErr != nil {
			return adapter.Change{}, readErr
		}
		if err = adapter.Command(ctx, root, "bun", "remove", exp.Name, "--ignore-scripts"); err != nil {
			return adapter.Change{}, err
		}
		if err = os.WriteFile(manifestPath, original, 0o644); err != nil {
			return adapter.Change{}, err
		}
	}
	change, err := a.Unwire(ctx, root, dep, exp)
	if err != nil {
		return change, err
	}
	return change, a.syncNative(ctx, root, exp)
}

func (a Adapter) Inspect(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	findings, err := a.Drift(ctx, root, dep, exp, locked)
	if err != nil || len(findings) != 0 {
		return findings, err
	}
	_, variant, err := a.Detect(root)
	if err != nil {
		return nil, err
	}
	if file, got, ok := npmLockedRevision(root, variant, exp.Name, locked.Commit); !ok {
		return []adapter.Finding{{File: file, Entry: exp.Name, Want: locked.Commit, Got: got, Repairable: true}}, nil
	}
	installed := filepath.Join(root, "node_modules", filepath.FromSlash(exp.Name))
	if _, err = os.Stat(installed); err == nil {
		return nil, nil
	}
	if variant == "yarn-berry" {
		if _, pnpErr := os.Stat(filepath.Join(root, ".pnp.cjs")); pnpErr == nil {
			return nil, nil
		}
	}
	return []adapter.Finding{{File: filepath.ToSlash(strings.TrimPrefix(installed, root+string(filepath.Separator))), Entry: exp.Name, Want: "installed dependency", Got: "missing", Repairable: true}}, nil
}

func npmLockedRevision(root string, variant adapter.Variant, name, commit string) (file, got string, ok bool) {
	switch variant {
	case "yarn-berry":
		file = "yarn.lock"
	case "pnpm":
		file = "pnpm-lock.yaml"
	case "bun":
		file = "bun.lock"
		if _, err := os.Stat(filepath.Join(root, file)); os.IsNotExist(err) {
			file = "bun.lockb"
		}
	default:
		file = "package-lock.json"
	}
	body, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		if os.IsNotExist(err) {
			return file, "missing", false
		}
		return file, err.Error(), false
	}
	if variant == "npm" {
		var document struct {
			Packages map[string]struct {
				Resolved string `json:"resolved"`
			} `json:"packages"`
		}
		if err := json.Unmarshal(body, &document); err != nil {
			return file, err.Error(), false
		}
		entry, exists := document.Packages["node_modules/"+name]
		if !exists {
			return file, "missing root package entry", false
		}
		if !strings.Contains(entry.Resolved, commit) {
			return file, entry.Resolved, false
		}
		return file, entry.Resolved, true
	}
	text := string(body)
	nameAt := strings.Index(text, name)
	if nameAt < 0 {
		return file, "missing root package entry", false
	}
	for from := nameAt; from >= 0; {
		start := from - 1024
		if start < 0 {
			start = 0
		}
		end := from + len(name) + 1024
		if end > len(text) {
			end = len(text)
		}
		if strings.Contains(text[start:end], commit) {
			return file, commit, true
		}
		next := strings.Index(text[from+len(name):], name)
		if next < 0 {
			break
		}
		from += len(name) + next
	}
	if !strings.Contains(text, commit) {
		return file, "locked at another revision", false
	}
	return file, "commit belongs to another lock entry", false
}

func (a Adapter) Drift(_ context.Context, root string, _ adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
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
	_, variant, err := a.Detect(root)
	if err != nil {
		return nil, err
	}
	want := dependencyURL(locked, string(variant), exp.Path)
	if got != want {
		return []adapter.Finding{{File: "package.json", Entry: exp.Name, Want: want, Got: got}}, nil
	}
	return nil, nil
}

func dependencyURL(locked adapter.Locked, variant, path string) string {
	url := locked.Git
	if strings.HasPrefix(url, "git@") {
		parts := strings.SplitN(url, ":", 2)
		if len(parts) == 2 {
			url = "ssh://" + parts[0] + "/" + parts[1]
		}
	}
	if !strings.HasPrefix(url, "git+") && !strings.HasPrefix(url, "git://") {
		url = "git+" + url
	}
	if variant == "yarn-berry" {
		url += "#commit=" + locked.Commit
		if path != "" && path != "." {
			url += "&workspace=" + path
		}
		return url
	}
	url += "#" + locked.Commit
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
	afterValue := removeEnd
	for afterValue < len(body) && strings.ContainsRune(" \t\r\n", rune(body[afterValue])) {
		afterValue++
	}
	if afterValue < len(body) && body[afterValue] == ',' {
		removeEnd = afterValue + 1
	} else {
		for removeStart > 0 && strings.ContainsRune(" \t\r\n", rune(body[removeStart-1])) {
			removeStart--
		}
		if removeStart > 0 && body[removeStart-1] == ',' {
			removeStart--
		}
	}
	body = body[:removeStart] + body[removeEnd:]
	next := []byte(string(b[:start+1]) + body + string(b[end:]))
	if strings.TrimSpace(body) == "" {
		next = removeEmptyTopLevelObject(next, "dependencies")
	}
	return next, true, nil
}

func removeEmptyTopLevelObject(b []byte, key string) []byte {
	start, end, ok := objectRange(b, key)
	if !ok || strings.TrimSpace(string(b[start+1:end])) != "" {
		return b
	}
	encoded, _ := json.Marshal(key)
	keyAt := strings.LastIndex(string(b[:start]), string(encoded))
	if keyAt < 0 {
		return b
	}
	removeStart, removeEnd := keyAt, end+1
	for removeStart > 0 && (b[removeStart-1] == ' ' || b[removeStart-1] == '\t') {
		removeStart--
	}
	afterValue := removeEnd
	for afterValue < len(b) && strings.ContainsRune(" \t\r\n", rune(b[afterValue])) {
		afterValue++
	}
	if afterValue < len(b) && b[afterValue] == ',' {
		removeEnd = afterValue + 1
	} else {
		for removeStart > 0 && strings.ContainsRune(" \t\r\n", rune(b[removeStart-1])) {
			removeStart--
		}
		if removeStart > 0 && b[removeStart-1] == ',' {
			removeStart--
		}
	}
	return append(append([]byte(nil), b[:removeStart]...), b[removeEnd:]...)
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
