package pypi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/neprel/git-a2a/internal/adapter"
	"github.com/neprel/git-a2a/internal/gitx"
)

type Adapter struct{}

func (Adapter) Ecosystem() string { return "pypi" }
func (Adapter) Detect(root string) (bool, adapter.Variant, error) {
	b, err := os.ReadFile(filepath.Join(root, "pyproject.toml"))
	if os.IsNotExist(err) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	s := string(b)
	if _, err := os.Stat(filepath.Join(root, "uv.lock")); err == nil || strings.Contains(s, "[tool.uv") {
		return true, "uv", nil
	}
	if _, err := os.Stat(filepath.Join(root, "poetry.lock")); err == nil {
		return true, "poetry", nil
	}
	if _, err := os.Stat(filepath.Join(root, "pdm.lock")); err == nil {
		return true, "pdm", nil
	}
	return true, "pep621", nil
}

func (a Adapter) Wire(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	ok, v, err := a.Detect(root)
	if err != nil || !ok {
		return adapter.Change{}, err
	}
	p := filepath.Join(root, "pyproject.toml")
	b, err := os.ReadFile(p)
	if err != nil {
		return adapter.Change{}, err
	}
	s := string(b)
	requirement := exp.Name
	if v != "uv" {
		requirement = fmt.Sprintf("%s @ git+%s@%s", exp.Name, dep.Git, pin(dep, locked))
	}
	next, changed, err := ensureProjectDependency(s, requirement, exp.Name)
	if err != nil {
		return adapter.Change{}, err
	}
	if v == "uv" {
		source := uvSource(dep, exp, locked)
		var c bool
		next, c = upsertUVSource(next, exp.Name, source)
		changed = changed || c
	}
	if changed {
		err = os.WriteFile(p, []byte(next), 0o644)
	}
	return adapter.Change{File: "pyproject.toml", Entry: exp.Name, Changed: changed}, err
}

func (a Adapter) Unwire(_ context.Context, root string, _ adapter.Dependency, exp adapter.Export) (adapter.Change, error) {
	p := filepath.Join(root, "pyproject.toml")
	b, err := os.ReadFile(p)
	if err != nil {
		return adapter.Change{}, err
	}
	s, changed := removeDependency(string(b), exp.Name)
	s, c := removeUVSource(s, exp.Name)
	changed = changed || c
	if changed {
		err = os.WriteFile(p, []byte(s), 0o644)
	}
	return adapter.Change{File: "pyproject.toml", Entry: exp.Name, Changed: changed}, err
}

func (a Adapter) Refresh(ctx context.Context, root string, _ adapter.Dependency, exp adapter.Export, _ adapter.Locked) error {
	_, v, err := a.Detect(root)
	if err != nil {
		return err
	}
	switch v {
	case "uv":
		return adapter.Command(ctx, root, "uv", "lock", "--upgrade-package", exp.Name)
	case "poetry":
		return adapter.Command(ctx, root, "poetry", "update", exp.Name)
	case "pdm":
		return adapter.Command(ctx, root, "pdm", "update", exp.Name)
	default:
		return nil
	}
}

func (a Adapter) Drift(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	b, err := os.ReadFile(filepath.Join(root, "pyproject.toml"))
	if err != nil {
		return nil, err
	}
	s := string(b)
	target := ""
	namePattern := regexp.MustCompile(`(?:["']` + regexp.QuoteMeta(exp.Name) + `["']\s*=|["']` + regexp.QuoteMeta(exp.Name) + `\s+@)`)
	for _, line := range strings.Split(s, "\n") {
		if namePattern.MatchString(line) {
			target = line
			break
		}
	}
	urlMatch := regexp.MustCompile(`git\s*=\s*["']([^"']+)|git\+([^"'\n]+)@[^"'\n]+`).FindStringSubmatch(target)
	gotURL := ""
	for _, v := range urlMatch[1:] {
		if v != "" {
			gotURL = v
			break
		}
	}
	badURL := gotURL == "" || gitx.NormalizeURL(gotURL) != gitx.NormalizeURL(locked.Git)
	badPin := dep.Track != "floating" && !strings.Contains(target, locked.Commit)
	if target == "" || badURL || badPin {
		return []adapter.Finding{{File: "pyproject.toml", Entry: exp.Name, Want: locked.Commit, Got: "missing or different revision"}}, nil
	}
	return nil, nil
}

func pin(dep adapter.Dependency, l adapter.Locked) string {
	if dep.Track == "floating" {
		return dep.Ref
	}
	return l.Commit
}
func uvSource(dep adapter.Dependency, exp adapter.Export, l adapter.Locked) string {
	field := "rev"
	if dep.Track == "floating" {
		field = "branch"
	}
	parts := []string{fmt.Sprintf("git = %q", dep.Git), fmt.Sprintf("%s = %q", field, pin(dep, l))}
	if exp.Path != "" && exp.Path != "." {
		parts = append(parts, fmt.Sprintf("subdirectory = %q", exp.Path))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func section(s, name string) (int, int, bool) {
	header := "[" + name + "]"
	start := strings.Index(s, header)
	if start < 0 {
		return 0, 0, false
	}
	body := start + len(header)
	rest := s[body:]
	end := len(s)
	if i := regexp.MustCompile(`(?m)^\s*\[[^\]]+\]`).FindStringIndex(rest); i != nil {
		end = body + i[0]
	}
	return body, end, true
}

func ensureProjectDependency(s, requirement, name string) (string, bool, error) {
	start, end, ok := section(s, "project")
	if !ok {
		return "", false, fmt.Errorf("pyproject.toml: [project] table is required")
	}
	body := s[start:end]
	re := regexp.MustCompile(`(?ms)(^\s*dependencies\s*=\s*\[)(.*?)(^\s*\])`)
	loc := re.FindStringSubmatchIndex(body)
	if loc == nil {
		return "", false, fmt.Errorf("pyproject.toml: [project].dependencies must be a multiline array")
	}
	items := body[loc[4]:loc[5]]
	nameRe := regexp.MustCompile(`(?m)^\s*["']` + regexp.QuoteMeta(name) + `(?:["' @<>=!~\[])`)
	if nameRe.MatchString(items) {
		return s, false, nil
	}
	indent := "  "
	if m := regexp.MustCompile(`(?m)^([ \t]*)["']`).FindStringSubmatch(items); len(m) > 1 {
		indent = m[1]
	}
	insert := indent + fmt.Sprintf("%q,\n", requirement)
	items += insert
	body = body[:loc[4]] + items + body[loc[5]:]
	return s[:start] + body + s[end:], true, nil
}

func upsertUVSource(s, name, value string) (string, bool) {
	key := fmt.Sprintf("%q", name)
	start, end, ok := section(s, "tool.uv.sources")
	line := key + " = " + value
	if !ok {
		sep := "\n"
		if strings.HasSuffix(s, "\n") {
			sep = ""
		}
		return s + sep + "\n[tool.uv.sources]\n" + line + "\n", true
	}
	body := s[start:end]
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `\s*=.*$`)
	if old := re.FindString(body); old != "" {
		if strings.TrimSpace(old) == line {
			return s, false
		}
		body = re.ReplaceAllString(body, line)
	} else {
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		body += line + "\n"
	}
	return s[:start] + body + s[end:], true
}

func removeDependency(s, name string) (string, bool) {
	start, end, ok := section(s, "project")
	if !ok {
		return s, false
	}
	body := s[start:end]
	re := regexp.MustCompile(`(?m)^\s*["']` + regexp.QuoteMeta(name) + `(?:["' @<>=!~\[])[^\n]*\n?`)
	if !re.MatchString(body) {
		return s, false
	}
	body = re.ReplaceAllString(body, "")
	return s[:start] + body + s[end:], true
}
func removeUVSource(s, name string) (string, bool) {
	start, end, ok := section(s, "tool.uv.sources")
	if !ok {
		return s, false
	}
	body := s[start:end]
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(fmt.Sprintf("%q", name)) + `\s*=.*\n?`)
	if !re.MatchString(body) {
		return s, false
	}
	body = re.ReplaceAllString(body, "")
	if strings.TrimSpace(body) == "" {
		header := strings.LastIndex(s[:start], "[tool.uv.sources]")
		if header >= 0 {
			if header > 0 && s[header-1] == '\n' {
				header--
			}
			return s[:header] + s[end:], true
		}
	}
	return s[:start] + body + s[end:], true
}
