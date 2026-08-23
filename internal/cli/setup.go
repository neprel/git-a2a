package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	lockfile "github.com/neprel/git-a2a/internal/lock"
	"github.com/neprel/git-a2a/internal/setupskill"
)

const (
	setupBegin = "<!-- git-a2a:skill:begin -->"
	setupEnd   = "<!-- git-a2a:skill:end -->"
)

var setupPointer = []byte(setupBegin + `
## git-a2a skill

Use [.agents/skills/git-a2a/SKILL.md](.agents/skills/git-a2a/SKILL.md) for git module dependencies,
module ownership, a2amodule.yml, and the managed dependency roster. Run ` + "`git-a2a usage`" + ` for the compact CLI briefing.
` + setupEnd + "\n")

type setupOptions struct {
	check, dryRun, all bool
	harnesses          []string
}
type setupFile struct {
	path, purpose string
	body          []byte
}

func (a *App) setup(args []string) int {
	o := setupOptions{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--check":
			o.check = true
		case "--dry-run":
			o.dryRun = true
		case "--all":
			o.all = true
		case "--harness":
			if i+1 >= len(args) {
				fmt.Fprintln(a.Err, "setup: --harness needs a comma-separated value")
				return 2
			}
			i++
			o.harnesses = append(o.harnesses, strings.Split(args[i], ",")...)
		default:
			if strings.HasPrefix(args[i], "--harness=") {
				o.harnesses = append(o.harnesses, strings.Split(strings.TrimPrefix(args[i], "--harness="), ",")...)
				continue
			}
			fmt.Fprintf(a.Err, "setup: unknown option %s\n", args[i])
			return 2
		}
	}
	if o.check && o.dryRun {
		fmt.Fprintln(a.Err, "setup: --check and --dry-run cannot be combined")
		return 2
	}
	if o.all && len(o.harnesses) > 0 {
		fmt.Fprintln(a.Err, "setup: --all and --harness cannot be combined")
		return 2
	}
	repoHarnesses, homeHarnesses := detectHarnesses(a.root(), a.home())
	harnesses := repoHarnesses
	if o.all {
		harnesses = allHarnessNames()
	} else if len(o.harnesses) > 0 {
		var selectErr error
		harnesses, selectErr = resolveHarnessNames(o.harnesses)
		if selectErr != nil {
			fmt.Fprintf(a.Err, "setup: %v\n", selectErr)
			return 2
		}
	}
	files, err := a.setupFiles(harnesses)
	if err != nil {
		fmt.Fprintf(a.Err, "setup: %v\n", err)
		return 1
	}
	if len(harnesses) == 0 {
		fmt.Fprintln(a.Err, "setup: no supported harness detected; installing cross-agent guidance only")
	} else {
		fmt.Fprintf(a.Err, "setup: detected %s\n", strings.Join(harnesses, ", "))
	}
	for _, name := range homeHarnesses {
		if contains(repoHarnesses, name) || contains(harnesses, name) {
			continue
		}
		fmt.Fprintf(a.Err, "setup: %s detected on this machine; pass --harness %s to configure it in this repository\n", name, harnessSlug(name))
	}
	for _, instruction := range externalMCPInstructions(harnesses) {
		fmt.Fprintf(a.Err, "setup: %s\n", instruction)
	}
	drift := 0
	for _, file := range files {
		current, err := os.ReadFile(file.path)
		if err == nil && bytes.Equal(current, file.body) {
			fmt.Fprintf(a.Out, "current %s (%s)\n", displayPath(a.root(), file.path), file.purpose)
			continue
		}
		if err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(a.Err, "setup: %s: %v\n", displayPath(a.root(), file.path), err)
			return 1
		}
		drift++
		verb := "write"
		if o.check {
			verb = "missing"
		} else if o.dryRun {
			verb = "would write"
		}
		fmt.Fprintf(a.Out, "%s %s (%s)\n", verb, displayPath(a.root(), file.path), file.purpose)
		if o.check || o.dryRun {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(file.path), 0o755); err != nil {
			fmt.Fprintf(a.Err, "setup: %s: %v\n", displayPath(a.root(), file.path), err)
			return 1
		}
		if err := lockfile.Atomic(file.path, file.body, 0o644); err != nil {
			fmt.Fprintf(a.Err, "setup: %s: %v\n", displayPath(a.root(), file.path), err)
			return 1
		}
	}
	if o.check && drift > 0 {
		fmt.Fprintf(a.Err, "setup: %d file(s) missing or stale\n", drift)
		return 1
	}
	if o.dryRun {
		fmt.Fprintf(a.Err, "setup: dry run; %d file(s) would change\n", drift)
	} else if o.check {
		fmt.Fprintln(a.Err, "setup: agent guidance is current")
	} else {
		fmt.Fprintf(a.Err, "setup: wrote %d file(s); git-a2a itself was not installed\n", drift)
	}
	return 0
}

func (a *App) setupFiles(harnesses []string) ([]setupFile, error) {
	root := a.root()
	files := make([]setupFile, 0, 12)
	addSkill := func(target, purpose string) error {
		return fs.WalkDir(setupskill.Files, "thin", func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			body, readErr := setupskill.Files.ReadFile(name)
			if readErr != nil {
				return readErr
			}
			rel, _ := filepath.Rel("thin", name)
			files = append(files, setupFile{filepath.Join(root, target, rel), purpose, body})
			return nil
		})
	}
	if err := addSkill(filepath.FromSlash(".agents/skills/git-a2a"), "cross-agent skill"); err != nil {
		return nil, err
	}
	if contains(harnesses, "Claude Code") {
		if err := addSkill(filepath.FromSlash(".claude/skills/git-a2a"), "Claude Code skill"); err != nil {
			return nil, err
		}
	}
	agents, err := managedSetupFile(filepath.Join(root, "AGENTS.md"), setupPointer)
	if err != nil {
		return nil, err
	}
	files = append(files, setupFile{filepath.Join(root, "AGENTS.md"), "skill pointer", agents})
	configs := []struct {
		harness, path, key string
		value              map[string]any
	}{
		{"Cursor", ".cursor/mcp.json", "mcpServers", stdioConfig()},
		{"GitHub Copilot", ".vscode/mcp.json", "servers", stdioConfig()},
		{"Gemini CLI", ".gemini/settings.json", "mcpServers", stdioConfig()},
	}
	if contains(harnesses, "Claude Code") || contains(harnesses, "GitHub Copilot") {
		path := filepath.Join(root, ".mcp.json")
		body, mergeErr := mergeJSONConfig(path, []string{"mcpServers"}, stdioConfig())
		if mergeErr != nil {
			return nil, fmt.Errorf(".mcp.json: %w", mergeErr)
		}
		purpose := "GitHub Copilot CLI MCP server"
		if contains(harnesses, "Claude Code") && contains(harnesses, "GitHub Copilot") {
			purpose = "Claude Code / GitHub Copilot CLI MCP server"
		} else if contains(harnesses, "Claude Code") {
			purpose = "Claude Code MCP server"
		}
		files = append(files, setupFile{path, purpose, body})
	}
	for _, config := range configs {
		if !contains(harnesses, config.harness) {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(config.path))
		body, mergeErr := mergeJSONConfig(path, []string{config.key}, config.value)
		if mergeErr != nil {
			return nil, fmt.Errorf("%s: %w", config.path, mergeErr)
		}
		files = append(files, setupFile{path, config.harness + " MCP server", body})
	}
	if contains(harnesses, "Codex") {
		path := filepath.Join(root, ".codex", "config.toml")
		body, mergeErr := managedTextFile(path, "# git-a2a:mcp:begin", "# git-a2a:mcp:end", []byte("# git-a2a:mcp:begin\n[mcp_servers.git-a2a]\ncommand = \"git-a2a\"\nargs = [\"mcp\"]\n# git-a2a:mcp:end\n"))
		if mergeErr != nil {
			return nil, mergeErr
		}
		files = append(files, setupFile{path, "Codex MCP server", body})
	}
	if contains(harnesses, "OpenCode") {
		path := filepath.Join(root, "opencode.json")
		body, mergeErr := mergeJSONConfig(path, []string{"mcp", "servers"}, map[string]any{"type": "local", "command": []any{"git-a2a", "mcp"}})
		if mergeErr != nil {
			return nil, fmt.Errorf("opencode.json: %w", mergeErr)
		}
		files = append(files, setupFile{path, "OpenCode MCP server", body})
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].path < files[j].path })
	return files, nil
}

func stdioConfig() map[string]any { return map[string]any{"command": "git-a2a", "args": []any{"mcp"}} }

type harnessSpec struct {
	slug, name string
	repo, home []string
}

var harnessSpecs = []harnessSpec{
	{"claude-code", "Claude Code", []string{".claude", "CLAUDE.md"}, []string{".claude"}},
	{"codex", "Codex", []string{".codex"}, []string{".codex"}},
	{"cursor", "Cursor", []string{".cursor", ".cursorrules"}, []string{".cursor"}},
	{"copilot", "GitHub Copilot", []string{filepath.Join(".github", "copilot-instructions.md"), filepath.Join(".github", "agents"), filepath.Join(".vscode", "mcp.json")}, []string{".copilot", filepath.Join(".config", "github-copilot")}},
	{"gemini", "Gemini CLI", []string{".gemini", "GEMINI.md"}, []string{".gemini"}},
	{"opencode", "OpenCode", []string{".opencode", "opencode.json", "opencode.jsonc"}, []string{filepath.Join(".config", "opencode")}},
	{"hermes", "Hermes Agent", []string{".hermes"}, []string{".hermes"}},
	{"openclaw", "OpenClaw", []string{".openclaw"}, []string{".openclaw"}},
}

func (a *App) home() string {
	if a.Home != "" {
		return a.Home
	}
	home, _ := os.UserHomeDir()
	return home
}

func detectHarnesses(root, home string) (repoFound, homeFound []string) {
	for _, candidate := range harnessSpecs {
		if anyMarker(root, candidate.repo) {
			repoFound = append(repoFound, candidate.name)
		}
		if anyMarker(home, candidate.home) {
			homeFound = append(homeFound, candidate.name)
		}
	}
	return repoFound, homeFound
}

func anyMarker(base string, markers []string) bool {
	for _, marker := range markers {
		if _, err := os.Stat(filepath.Join(base, marker)); err == nil {
			return true
		}
	}
	return false
}

func resolveHarnessNames(values []string) ([]string, error) {
	var resolved []string
	for _, raw := range values {
		wanted := strings.ToLower(strings.TrimSpace(raw))
		found := ""
		for _, candidate := range harnessSpecs {
			if wanted == candidate.slug || wanted == strings.ToLower(candidate.name) {
				found = candidate.name
				break
			}
		}
		if found == "" {
			return nil, fmt.Errorf("unknown harness %q", raw)
		}
		if !contains(resolved, found) {
			resolved = append(resolved, found)
		}
	}
	return resolved, nil
}

func allHarnessNames() []string {
	result := make([]string, 0, len(harnessSpecs))
	for _, candidate := range harnessSpecs {
		result = append(result, candidate.name)
	}
	return result
}

func harnessSlug(name string) string {
	for _, candidate := range harnessSpecs {
		if candidate.name == name {
			return candidate.slug
		}
	}
	return name
}

func externalMCPInstructions(harnesses []string) []string {
	var instructions []string
	if contains(harnesses, "Hermes Agent") {
		instructions = append(instructions, "Hermes Agent keeps MCP config in user scope; run: hermes mcp add git-a2a --command git-a2a --args mcp")
	}
	if contains(harnesses, "OpenClaw") {
		instructions = append(instructions, `OpenClaw keeps MCP config in user scope; run: openclaw mcp set git-a2a '{"command":"git-a2a","args":["mcp"]}'`)
	}
	return instructions
}

func mergeJSONConfig(path string, parents []string, server map[string]any) ([]byte, error) {
	body := []byte("{}\n")
	root := map[string]any{}
	if existing, err := os.ReadFile(path); err == nil {
		body = existing
		if err := json.Unmarshal(existing, &root); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	keys := append(append([]string{}, parents...), "git-a2a")
	current := any(root)
	for _, key := range keys {
		object, ok := current.(map[string]any)
		if !ok {
			break
		}
		next, found := object[key]
		if !found {
			current = nil
			break
		}
		current = next
	}
	if reflect.DeepEqual(current, any(server)) {
		return body, nil
	}
	return upsertJSON(body, 0, len(body), keys, server, 0)
}

func upsertJSON(body []byte, start, end int, keys []string, value any, depth int) ([]byte, error) {
	open := skipJSONSpace(body, start, end)
	if open >= end || body[open] != '{' {
		return nil, fmt.Errorf("%s must be an object", strings.Join(keys, "."))
	}
	close, err := scanJSONValue(body, open, end)
	if err != nil {
		return nil, err
	}
	close--
	valueStart, valueEnd, found, err := findJSONProperty(body, open, close, keys[0])
	if err != nil {
		return nil, err
	}
	if !found {
		insert := value
		for i := len(keys) - 1; i > 0; i-- {
			insert = map[string]any{keys[i]: insert}
		}
		return insertJSONProperty(body, open, close, keys[0], insert, depth)
	}
	if len(keys) > 1 {
		return upsertJSON(body, valueStart, valueEnd, keys[1:], value, depth+1)
	}
	replacement, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(append(append([]byte{}, body[:valueStart]...), replacement...), body[valueEnd:]...), nil
}

func findJSONProperty(body []byte, open, close int, wanted string) (int, int, bool, error) {
	position := open + 1
	for {
		position = skipJSONSpace(body, position, close)
		if position >= close {
			return 0, 0, false, nil
		}
		if body[position] == ',' {
			position++
			continue
		}
		if body[position] != '"' {
			return 0, 0, false, fmt.Errorf("invalid JSON object")
		}
		keyEnd, err := scanJSONString(body, position, close)
		if err != nil {
			return 0, 0, false, err
		}
		var key string
		if err := json.Unmarshal(body[position:keyEnd], &key); err != nil {
			return 0, 0, false, err
		}
		position = skipJSONSpace(body, keyEnd, close)
		if position >= close || body[position] != ':' {
			return 0, 0, false, fmt.Errorf("invalid JSON object")
		}
		valueStart := skipJSONSpace(body, position+1, close)
		valueEnd, err := scanJSONValue(body, valueStart, close)
		if err != nil {
			return 0, 0, false, err
		}
		if key == wanted {
			return valueStart, valueEnd, true, nil
		}
		position = valueEnd
	}
}

func scanJSONValue(body []byte, start, end int) (int, error) {
	start = skipJSONSpace(body, start, end)
	if start >= end {
		return 0, fmt.Errorf("missing JSON value")
	}
	if body[start] == '"' {
		return scanJSONString(body, start, end)
	}
	if body[start] == '{' || body[start] == '[' {
		opening, closing, level := body[start], byte('}'), 0
		if opening == '[' {
			closing = ']'
		}
		for i := start; i < end; i++ {
			if body[i] == '"' {
				next, err := scanJSONString(body, i, end)
				if err != nil {
					return 0, err
				}
				i = next - 1
				continue
			}
			if body[i] == opening {
				level++
			}
			if body[i] == closing {
				level--
				if level == 0 {
					return i + 1, nil
				}
			}
		}
		return 0, fmt.Errorf("unterminated JSON value")
	}
	position := start
	for position < end && body[position] != ',' && body[position] != '}' && body[position] != ']' {
		position++
	}
	for position > start && (body[position-1] == ' ' || body[position-1] == '\t' || body[position-1] == '\r' || body[position-1] == '\n') {
		position--
	}
	return position, nil
}

func scanJSONString(body []byte, start, end int) (int, error) {
	for i := start + 1; i < end; i++ {
		if body[i] == '\\' {
			i++
			continue
		}
		if body[i] == '"' {
			return i + 1, nil
		}
	}
	return 0, fmt.Errorf("unterminated JSON string")
}

func skipJSONSpace(body []byte, start, end int) int {
	for start < end && (body[start] == ' ' || body[start] == '\t' || body[start] == '\r' || body[start] == '\n') {
		start++
	}
	return start
}

func insertJSONProperty(body []byte, open, close int, key string, value any, depth int) ([]byte, error) {
	encodedKey, _ := json.Marshal(key)
	encodedValue, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	nonSpace := close - 1
	for nonSpace > open && (body[nonSpace] == ' ' || body[nonSpace] == '\t' || body[nonSpace] == '\r' || body[nonSpace] == '\n') {
		nonSpace--
	}
	empty := nonSpace == open
	multiline := bytes.Contains(body[open:close], []byte("\n")) || empty
	var insertion []byte
	insertAt := close
	if multiline {
		insertAt = nonSpace + 1
		indent := strings.Repeat("  ", depth+1)
		closingIndent := strings.Repeat("  ", depth)
		separator := ""
		if !empty {
			separator = ","
		}
		insertion = []byte(separator + "\n" + indent + string(encodedKey) + ": " + string(encodedValue) + "\n" + closingIndent)
	} else {
		separator := ""
		if !empty {
			separator = ","
		}
		insertion = []byte(separator + string(encodedKey) + ":" + string(encodedValue))
	}
	return append(append(append([]byte{}, body[:insertAt]...), insertion...), body[close:]...), nil
}

func managedSetupFile(path string, block []byte) ([]byte, error) {
	return managedTextFile(path, setupBegin, setupEnd, block)
}
func managedTextFile(path, begin, end string, block []byte) ([]byte, error) {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	text := string(existing)
	start, finish := strings.Index(text, begin), strings.Index(text, end)
	if (start < 0) != (finish < 0) {
		return nil, fmt.Errorf("%s has a partial git-a2a setup block", displayPath(filepath.Dir(path), path))
	}
	if start >= 0 {
		finish += len(end)
		if finish < len(text) && text[finish] == '\n' {
			finish++
		}
		return []byte(text[:start] + string(block) + text[finish:]), nil
	}
	if len(existing) == 0 {
		return block, nil
	}
	if !bytes.HasSuffix(existing, []byte("\n")) {
		existing = append(existing, '\n')
	}
	return append(append(existing, '\n'), block...), nil
}

func displayPath(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
