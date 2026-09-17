package pypi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"unicode"

	"github.com/neprel/git-a2a/v2/internal/adapter"
)

type Adapter struct{}

func (Adapter) Ecosystem() string { return "pypi" }

func (a Adapter) Capability(root string, _ adapter.Dependency, exp adapter.Export) error {
	ok, variant, err := a.Detect(root)
	if err != nil {
		return err
	}
	if !ok {
		return adapter.NotWirable("Python consumer manifest is absent")
	}
	if exp.Path != "" && exp.Path != "." && variant != "uv" {
		return adapter.NotWirable(fmt.Sprintf("%s cannot express subdirectory %s", variant, exp.Path))
	}
	return nil
}
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
	if _, err := os.Stat(filepath.Join(root, "poetry.lock")); err == nil || regexp.MustCompile(`(?m)^\[tool\.poetry\.dependencies\][ \t]*$`).MatchString(s) {
		return true, "poetry", nil
	}
	if _, err := os.Stat(filepath.Join(root, "pdm.lock")); err == nil || strings.Contains(s, "[tool.pdm") {
		return true, "pdm", nil
	}
	return true, "pep621", nil
}

func (a Adapter) Wire(_ context.Context, root string, _ adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
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
	if exp.Path != "" && exp.Path != "." && v != "uv" {
		return adapter.Change{}, adapter.NotWirable(fmt.Sprintf("%s cannot express subdirectory %s", v, exp.Path))
	}
	if v == "poetry" {
		value := fmt.Sprintf("{ git = %q, rev = %q }", locked.Git, locked.Commit)
		next, changed, err := upsertTableEntry(s, "tool.poetry.dependencies", exp.Name, value)
		if err == nil && changed {
			err = os.WriteFile(p, []byte(next), 0o644)
		}
		return adapter.Change{File: "pyproject.toml", Entry: exp.Name, Changed: changed}, err
	}
	requirement := exp.Name
	if v != "uv" {
		requirement = fmt.Sprintf("%s @ git+%s@%s", exp.Name, locked.Git, locked.Commit)
	}
	next, changed, err := ensureProjectDependency(s, requirement, exp.Name)
	if err != nil {
		return adapter.Change{}, err
	}
	if v == "uv" {
		source := uvSource(exp, locked)
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
	s, c = removeTableEntry(s, "tool.poetry.dependencies", exp.Name)
	changed = changed || c
	if changed {
		err = os.WriteFile(p, []byte(s), 0o644)
	}
	return adapter.Change{File: "pyproject.toml", Entry: exp.Name, Changed: changed}, err
}

func (a Adapter) syncNative(ctx context.Context, root string, exp adapter.Export) error {
	_, v, err := a.Detect(root)
	if err != nil {
		return err
	}
	if err := adapter.RequireTool(ctx, a.Ecosystem(), v); err != nil {
		return err
	}
	switch v {
	case "uv":
		return adapter.Command(ctx, root, "uv", "sync", "--no-install-project")
	case "poetry":
		// install/sync deliberately refuses a stale poetry.lock. Reconcile the
		// changed direct requirement first; Poetry 2.x lock reuses unchanged
		// locked packages by default instead of upgrading the whole project.
		if err := adapter.Command(ctx, root, "poetry", "lock", "--no-interaction"); err != nil {
			return err
		}
		return adapter.Command(ctx, root, "poetry", "sync", "--no-root", "--no-interaction")
	case "pdm":
		if err := ensurePDMEnvironment(ctx, root); err != nil {
			return err
		}
		// pdm sync only warns when the content hash is stale and proceeds with
		// the old lock. A targeted update is also required for a same-version
		// VCS commit change: `pdm lock --update-reuse` intentionally reuses the
		// old VCS package.
		return adapter.Command(ctx, root, "pdm", "update", "--no-self", "--update-reuse", exp.Name)
	case "pep621":
		venvPython := venvPythonPath(root)
		if _, statErr := os.Stat(venvPython); os.IsNotExist(statErr) {
			if err := adapter.Command(ctx, root, "python3", "-m", "venv", ".venv"); err != nil {
				return err
			}
		}
		// A VCS dependency may keep the same package version while its locked
		// commit changes. pip otherwise considers the installed distribution
		// satisfied, so force only this direct requirement through resolution.
		project := mustRead(root)
		direct := pipVCSRequirement(projectRequirement(project, exp.Name))
		if err := adapter.Command(ctx, root, venvPython, "-m", "pip", "install", "--force-reinstall", "--disable-pip-version-check", direct); err != nil {
			return err
		}
		// A recreated environment must also regain the consumer's unrelated
		// direct requirements. A normal second install keeps already-satisfying
		// versions instead of broadly upgrading or force-reinstalling them.
		var requirements []string
		for _, requirement := range projectRequirements(project) {
			if normalizeProjectName(requirementName(requirement)) != normalizeProjectName(exp.Name) {
				requirements = append(requirements, requirement)
			}
		}
		if len(requirements) == 0 {
			return nil
		}
		args := []string{"-m", "pip", "install", "--disable-pip-version-check"}
		args = append(args, requirements...)
		return adapter.Command(ctx, root, venvPython, args...)
	default:
		return nil
	}
}

func ensurePDMEnvironment(ctx context.Context, root string) error {
	selected, err := os.ReadFile(filepath.Join(root, ".pdm-python"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	python := strings.TrimSpace(string(selected))
	if python == "" {
		return nil
	}
	if !filepath.IsAbs(python) {
		python = filepath.Join(root, python)
	}
	if fileExists(python) {
		return nil
	}
	// .pdm-python is project-local ownership evidence for the selected
	// interpreter. Recreate only a missing conventional venv target; never
	// delete, clean, or retarget an existing external/shared environment.
	wantExecutable := "python"
	if runtime.GOOS == "windows" {
		wantExecutable = "python.exe"
	}
	if !strings.EqualFold(filepath.Base(python), wantExecutable) {
		return fmt.Errorf("pdm selected interpreter %q is missing and is not a conventional venv target", python)
	}
	binDir := filepath.Dir(python)
	if runtime.GOOS == "windows" {
		if !strings.EqualFold(filepath.Base(binDir), "Scripts") {
			return fmt.Errorf("pdm selected interpreter %q is missing and is not a conventional venv target", python)
		}
	} else if filepath.Base(binDir) != "bin" {
		return fmt.Errorf("pdm selected interpreter %q is missing and is not a conventional venv target", python)
	}
	environment := filepath.Dir(binDir)
	if environment == "." || environment == string(filepath.Separator) {
		return fmt.Errorf("refusing to recreate unsafe PDM environment %q", environment)
	}
	interpreter := "python3"
	if runtime.GOOS == "windows" {
		interpreter = "python"
	}
	return adapter.Command(ctx, root, interpreter, "-m", "venv", environment)
}

func (a Adapter) Pull(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	change, err := a.Wire(ctx, root, dep, exp, locked)
	if err != nil {
		return change, err
	}
	return change, a.syncNative(ctx, root, exp)
}

func (a Adapter) Remove(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	_, variant, detectErr := a.Detect(root)
	if detectErr != nil {
		return adapter.Change{}, detectErr
	}
	if variant == "pdm" {
		if err := adapter.RequireTool(ctx, a.Ecosystem(), variant); err != nil {
			return adapter.Change{}, err
		}
		// PDM's plain sync does not remove packages dropped from the lock.
		// Its native remove command owns declaration, lock, and environment as
		// one targeted operation and preserves other selected roots.
		err := adapter.Command(ctx, root, "pdm", "remove", "--no-self", exp.Name)
		return adapter.Change{File: "pyproject.toml", Entry: exp.Name, Changed: err == nil}, err
	}
	change, err := a.Unwire(ctx, root, dep, exp)
	if err != nil {
		return change, err
	}
	if variant == "pep621" {
		venvPython := venvPythonPath(root)
		if _, statErr := os.Stat(venvPython); os.IsNotExist(statErr) {
			return change, nil
		}
		return change, adapter.Command(ctx, root, venvPython, "-m", "pip", "uninstall", "-y", exp.Name)
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
	if err := adapter.RequireTool(ctx, a.Ecosystem(), variant); err != nil {
		return nil, err
	}
	if variant != "pep621" {
		if finding := inspectPythonLock(root, variant, exp.Name, locked); finding != nil {
			return []adapter.Finding{*finding}, nil
		}
	}
	python, location, discoveryErr := projectPython(ctx, root, variant)
	if discoveryErr != nil {
		return []adapter.Finding{{File: location, Entry: exp.Name, Want: "project environment", Got: discoveryErr.Error(), Repairable: true}}, nil
	}
	if finding := inspectInstalledRevision(ctx, root, python, location, exp.Name, locked); finding != nil {
		return []adapter.Finding{*finding}, nil
	}
	return nil, nil
}

func mustRead(root string) string {
	b, _ := os.ReadFile(filepath.Join(root, "pyproject.toml"))
	return string(b)
}

func venvPythonPath(root string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(root, ".venv", "Scripts", "python.exe")
	}
	return filepath.Join(root, ".venv", "bin", "python")
}

func projectPython(ctx context.Context, root string, variant adapter.Variant) (python, location string, err error) {
	location = string(variant) + " project environment"
	switch variant {
	case "uv":
		environment := os.Getenv("UV_PROJECT_ENVIRONMENT")
		if environment == "" {
			environment = filepath.Join(root, ".venv")
		} else if !filepath.IsAbs(environment) {
			environment = filepath.Join(root, environment)
		}
		python = environmentPythonPath(environment)
	case "poetry":
		output, commandErr := adapter.CommandOutput(ctx, root, "poetry", "env", "info", "--executable")
		if commandErr != nil {
			return "", location, fmt.Errorf("missing: %w", commandErr)
		}
		python = interpreterFromOutput(root, output)
	case "pdm":
		selected, readErr := os.ReadFile(filepath.Join(root, ".pdm-python"))
		if readErr == nil {
			python = strings.TrimSpace(string(selected))
			if !filepath.IsAbs(python) {
				python = filepath.Join(root, python)
			}
		} else if !os.IsNotExist(readErr) {
			return "", location, readErr
		} else if local := environmentPythonPath(filepath.Join(root, ".venv")); fileExists(local) {
			python = local
		} else {
			// `pdm info --python` only reports the interpreter selected for the
			// project. It does not resolve or synchronize dependencies.
			output, commandErr := adapter.CommandOutput(ctx, root, "pdm", "info", "--python")
			if commandErr != nil {
				return "", location, fmt.Errorf("missing: %w", commandErr)
			}
			python = interpreterFromOutput(root, output)
		}
	default:
		python = venvPythonPath(root)
	}
	if python == "" || !fileExists(python) {
		return "", location, fmt.Errorf("missing interpreter %q", python)
	}
	return python, location, nil
}

func interpreterFromOutput(root string, output []byte) string {
	for _, line := range strings.Split(string(output), "\n") {
		candidate := strings.TrimSpace(line)
		if candidate == "" {
			continue
		}
		resolved := candidate
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(root, resolved)
		}
		if fileExists(resolved) {
			return resolved
		}
	}
	return strings.TrimSpace(string(output))
}

func environmentPythonPath(environment string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(environment, "Scripts", "python.exe")
	}
	return filepath.Join(environment, "bin", "python")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func inspectPythonLock(root string, variant adapter.Variant, name string, locked adapter.Locked) *adapter.Finding {
	lockName := map[adapter.Variant]string{"uv": "uv.lock", "poetry": "poetry.lock", "pdm": "pdm.lock"}[variant]
	body, err := os.ReadFile(filepath.Join(root, lockName))
	if err != nil {
		return &adapter.Finding{File: lockName, Entry: name, Want: "locked Git source " + locked.Git + " at " + locked.Commit, Got: "missing or unreadable", Repairable: true}
	}
	block := packageLockBlock(string(body), name)
	if block == "" {
		return &adapter.Finding{File: lockName, Entry: name, Want: "locked Git source " + locked.Git + " at " + locked.Commit, Got: "package entry missing", Repairable: true}
	}
	if !strings.Contains(block, locked.Commit) {
		return &adapter.Finding{File: lockName, Entry: name, Want: locked.Commit, Got: "different locked revision", Repairable: true}
	}
	if !blockContainsGitURL(block, locked.Git) {
		return &adapter.Finding{File: lockName, Entry: name, Want: locked.Git, Got: "different locked source", Repairable: true}
	}
	return nil
}

func packageLockBlock(body, name string) string {
	for _, block := range strings.Split(body, "[[package]]") {
		match := regexp.MustCompile(`(?m)^name[ \t]*=[ \t]*["']([^"']+)["'][ \t]*$`).FindStringSubmatch(block)
		if len(match) == 2 && normalizeProjectName(match[1]) == normalizeProjectName(name) {
			return block
		}
	}
	return ""
}

func blockContainsGitURL(block, want string) bool {
	want = canonicalGitURL(want)
	for _, match := range regexp.MustCompile(`(?:https?|ssh|git|file)://[^"' \t\r\n}]+`).FindAllString(block, -1) {
		candidate := strings.SplitN(match, "?", 2)[0]
		candidate = strings.SplitN(candidate, "#", 2)[0]
		if canonicalGitURL(candidate) == want {
			return true
		}
	}
	return false
}

func canonicalGitURL(value string) string {
	value = strings.TrimPrefix(strings.TrimSpace(value), "git+")
	value = strings.Replace(value, "file://None/", "file:///", 1)
	value = strings.TrimSuffix(value, "/")
	value = strings.TrimSuffix(value, ".git")
	return value
}

type directURL struct {
	URL     string `json:"url"`
	VCSInfo struct {
		CommitID string `json:"commit_id"`
	} `json:"vcs_info"`
}

func inspectInstalledRevision(ctx context.Context, root, python, location, name string, locked adapter.Locked) *adapter.Finding {
	const script = `import importlib.metadata as m, sys
d = m.distribution(sys.argv[1])
value = d.read_text("direct_url.json")
if not value:
    raise SystemExit("direct_url.json is missing")
print(value)`
	output, err := adapter.CommandOutput(ctx, root, python, "-I", "-c", script, name)
	if err != nil {
		return &adapter.Finding{File: location, Entry: name, Want: "installed from " + locked.Git + " at " + locked.Commit, Got: "missing or unreadable: " + err.Error(), Repairable: true}
	}
	var metadata directURL
	if err := json.Unmarshal(output, &metadata); err != nil {
		return &adapter.Finding{File: location, Entry: name, Want: "valid direct_url.json", Got: "invalid metadata: " + err.Error(), Repairable: true}
	}
	if canonicalGitURL(metadata.URL) != canonicalGitURL(locked.Git) {
		return &adapter.Finding{File: location, Entry: name, Want: locked.Git, Got: metadata.URL, Repairable: true}
	}
	if metadata.VCSInfo.CommitID != locked.Commit {
		return &adapter.Finding{File: location, Entry: name, Want: locked.Commit, Got: metadata.VCSInfo.CommitID, Repairable: true}
	}
	return nil
}

func (a Adapter) Drift(_ context.Context, root string, _ adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	b, err := os.ReadFile(filepath.Join(root, "pyproject.toml"))
	if err != nil {
		return nil, err
	}
	s := string(b)
	_, variant, err := a.Detect(root)
	if err != nil {
		return nil, err
	}
	var got, want string
	switch variant {
	case "uv":
		sourceEntry := tableEntry(s, "tool.uv.sources", exp.Name)
		want = strconv.Quote(exp.Name) + " = " + uvSource(exp, locked)
		requirement := projectRequirement(s, exp.Name)
		got = sourceEntry
		if requirement == "" && sourceEntry == "" {
			got = ""
		} else if requirement != exp.Name {
			got = strings.TrimSpace(requirement + " / " + sourceEntry)
		}
	case "poetry":
		got = tableEntry(s, "tool.poetry.dependencies", exp.Name)
		want = tomlKey(exp.Name) + fmt.Sprintf(" = { git = %q, rev = %q }", locked.Git, locked.Commit)
	default:
		got = projectRequirement(s, exp.Name)
		want = fmt.Sprintf("%s @ git+%s@%s", exp.Name, locked.Git, locked.Commit)
	}
	if got != want {
		return []adapter.Finding{{File: "pyproject.toml", Entry: exp.Name, Want: want, Got: got}}, nil
	}
	return nil, nil
}

func uvSource(exp adapter.Export, l adapter.Locked) string {
	parts := []string{fmt.Sprintf("git = %q", l.Git), fmt.Sprintf("rev = %q", l.Commit)}
	if exp.Path != "" && exp.Path != "." {
		parts = append(parts, fmt.Sprintf("subdirectory = %q", exp.Path))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func tableEntry(s, table, name string) string {
	start, end, ok := section(s, table)
	if !ok {
		return ""
	}
	re := regexp.MustCompile(`(?m)^[ \t]*` + tomlKeyPattern(name) + `[ \t]*=.*$`)
	return strings.TrimSpace(re.FindString(s[start:end]))
}

func projectRequirement(s, name string) string {
	for _, requirement := range projectRequirements(s) {
		if normalizeProjectName(requirementName(requirement)) == normalizeProjectName(name) {
			return requirement
		}
	}
	return ""
}

func pipVCSRequirement(requirement string) string {
	if _, vcs, ok := strings.Cut(requirement, " @ "); ok && strings.HasPrefix(vcs, "git+") {
		return vcs
	}
	return requirement
}

func projectRequirements(s string) []string {
	start, end, ok := section(s, "project")
	if !ok {
		return nil
	}
	_, open, close, ok := dependencyArrayRange(s[start:end])
	if !ok {
		return nil
	}
	var requirements []string
	for _, item := range quotedItems(s[start+open+1 : start+close]) {
		requirements = append(requirements, item.value)
	}
	return requirements
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
	prefixStart, open, close, ok := dependencyArrayRange(body)
	if !ok {
		block := "\n# git-a2a:begin dependencies " + name + "\ndependencies = [" + strconv.Quote(requirement) + "]\n# git-a2a:end dependencies " + name + "\n"
		return s[:end] + block + s[end:], true, nil
	}
	items := body[open+1 : close]
	for _, item := range quotedItems(items) {
		if normalizeProjectName(requirementName(item.value)) != normalizeProjectName(name) {
			continue
		}
		if item.value == requirement {
			return s, false, nil
		}
		items = items[:item.start] + strconv.Quote(requirement) + items[item.end:]
		body = body[:open+1] + items + body[close:]
		return s[:start] + body + s[end:], true, nil
	}
	if !strings.Contains(items, "\n") {
		var values []string
		for _, match := range regexp.MustCompile(`["']([^"']*)["']`).FindAllStringSubmatch(items, -1) {
			values = append(values, match[1])
		}
		values = append(values, requirement)
		lineStart := strings.LastIndex(body[:prefixStart], "\n") + 1
		baseIndent := body[lineStart:prefixStart]
		var block strings.Builder
		block.WriteString(body[prefixStart : open+1])
		block.WriteByte('\n')
		for _, value := range values {
			fmt.Fprintf(&block, "%s  %q,\n", baseIndent, value)
		}
		block.WriteString(baseIndent)
		block.WriteByte(']')
		body = body[:prefixStart] + block.String() + body[close+1:]
		return s[:start] + body + s[end:], true, nil
	}
	indent := "  "
	if m := regexp.MustCompile(`(?m)^([ \t]*)["']`).FindStringSubmatch(items); len(m) > 1 {
		indent = m[1]
	}
	insert := indent + fmt.Sprintf("%q,\n", requirement)
	items += insert
	body = body[:open+1] + items + body[close:]
	return s[:start] + body + s[end:], true, nil
}

type quotedItem struct {
	start, end int
	value      string
}

func quotedItems(value string) []quotedItem {
	var out []quotedItem
	for i := 0; i < len(value); i++ {
		if value[i] != '\'' && value[i] != '"' {
			continue
		}
		quote := value[i]
		start := i
		var content strings.Builder
		for i++; i < len(value); i++ {
			if value[i] == '\\' && quote == '"' && i+1 < len(value) {
				content.WriteByte(value[i])
				i++
				content.WriteByte(value[i])
				continue
			}
			if value[i] == quote {
				raw := string(quote) + content.String() + string(quote)
				decoded := content.String()
				if quote == '"' {
					if unquoted, err := strconv.Unquote(raw); err == nil {
						decoded = unquoted
					}
				}
				out = append(out, quotedItem{start: start, end: i + 1, value: decoded})
				break
			}
			content.WriteByte(value[i])
		}
	}
	return out
}

func requirementName(requirement string) string {
	for index, char := range requirement {
		if unicode.IsSpace(char) || strings.ContainsRune("@[<>=!~", char) {
			return requirement[:index]
		}
	}
	return requirement
}

func normalizeProjectName(name string) string {
	return strings.NewReplacer("_", "-", ".", "-").Replace(strings.ToLower(name))
}

func dependencyArrayRange(body string) (prefixStart, open, close int, ok bool) {
	loc := regexp.MustCompile(`(?m)^\s*dependencies\s*=\s*\[`).FindStringIndex(body)
	if loc == nil {
		return 0, 0, 0, false
	}
	open = strings.LastIndex(body[loc[0]:loc[1]], "[") + loc[0]
	quote := byte(0)
	escaped := false
	for i := open + 1; i < len(body); i++ {
		c := body[i]
		if quote != 0 {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c == ']' {
			return loc[0], open, i, true
		}
	}
	return 0, 0, 0, false
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
	re := regexp.MustCompile(`(?m)^[ \t]*` + tomlKeyPattern(name) + `[ \t]*=.*$`)
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
	managed := regexp.MustCompile(`(?ms)\n# git-a2a:begin dependencies ` + regexp.QuoteMeta(name) + `\n.*?^# git-a2a:end dependencies ` + regexp.QuoteMeta(name) + `\n`)
	if managed.MatchString(s) {
		return managed.ReplaceAllString(s, ""), true
	}
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

func upsertTableEntry(s, table, name, value string) (string, bool, error) {
	start, end, ok := section(s, table)
	if !ok {
		return "", false, fmt.Errorf("pyproject.toml: [%s] table is required", table)
	}
	body := s[start:end]
	line := tomlKey(name) + " = " + value
	re := regexp.MustCompile(`(?m)^[ \t]*` + tomlKeyPattern(name) + `[ \t]*=.*$`)
	if old := re.FindString(body); old != "" {
		if strings.TrimSpace(old) == line {
			return s, false, nil
		}
		body = re.ReplaceAllString(body, line)
	} else {
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		body += line + "\n"
	}
	return s[:start] + body + s[end:], true, nil
}

func removeTableEntry(s, table, name string) (string, bool) {
	start, end, ok := section(s, table)
	if !ok {
		return s, false
	}
	body := s[start:end]
	re := regexp.MustCompile(`(?m)^[ \t]*` + tomlKeyPattern(name) + `[ \t]*=.*\n?`)
	if !re.MatchString(body) {
		return s, false
	}
	body = re.ReplaceAllString(body, "")
	return s[:start] + body + s[end:], true
}

func tomlKey(name string) string {
	if regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(name) {
		return name
	}
	return strconv.Quote(name)
}
func removeUVSource(s, name string) (string, bool) {
	start, end, ok := section(s, "tool.uv.sources")
	if !ok {
		return s, false
	}
	body := s[start:end]
	re := regexp.MustCompile(`(?m)^[ \t]*` + tomlKeyPattern(name) + `[ \t]*=.*\n?`)
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

func tomlKeyPattern(name string) string {
	patterns := []string{
		regexp.QuoteMeta(strconv.Quote(name)),
		regexp.QuoteMeta("'" + strings.ReplaceAll(name, "'", "\\'") + "'"),
	}
	if regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(name) {
		patterns = append(patterns, regexp.QuoteMeta(name))
	}
	return `(?:` + strings.Join(patterns, `|`) + `)`
}
