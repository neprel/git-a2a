package meson

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/neprel/git-a2a/v2/internal/adapter"
)

const (
	rootFile    = "meson.build"
	regionBegin = "# git-a2a:begin dependencies\n"
	regionEnd   = "# git-a2a:end dependencies\n"
)

type Adapter struct{}

func (Adapter) Ecosystem() string { return "meson" }

func (Adapter) Detect(root string) (bool, adapter.Variant, error) {
	_, err := os.Stat(filepath.Join(root, rootFile))
	if os.IsNotExist(err) {
		return false, "", nil
	}
	return err == nil, "meson", err
}

func (a Adapter) wire(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, _ adapter.Locked) (adapter.Change, error) {
	if exp.Path == "" || exp.Path == "." {
		return adapter.Change{}, adapter.NotWirable("Meson integration requires a materialized source checkout path")
	}
	if ok, _, err := a.Detect(root); err != nil || !ok {
		return adapter.Change{}, err
	}
	path := filepath.Join(root, rootFile)
	before, err := os.ReadFile(path)
	if err != nil {
		return adapter.Change{}, err
	}
	if malformedRegion(before) {
		return adapter.Change{}, fmt.Errorf("%s contains a malformed git-a2a dependency region", rootFile)
	}
	blocks, foreign := parseRegion(before)
	blocks[dep.Name] = sourcePath(exp)
	after := upsertRegion(before, renderRegion(blocks))
	if string(before) == string(after) {
		return adapter.Change{File: rootFile, Entry: dep.Name}, nil
	}
	if err = os.WriteFile(path, after, 0o644); err != nil {
		return adapter.Change{}, err
	}
	warning := ""
	if foreign {
		warning = "the git-a2a-owned Meson dependency region contained foreign content; git-a2a discarded it"
	}
	return adapter.Change{File: rootFile, Entry: dep.Name, Changed: true, Warning: warning}, nil
}

func (Adapter) unwire(_ context.Context, root string, dep adapter.Dependency, _ adapter.Export) (adapter.Change, error) {
	path := filepath.Join(root, rootFile)
	before, err := os.ReadFile(path)
	if err != nil {
		return adapter.Change{}, err
	}
	if malformedRegion(before) {
		return adapter.Change{}, fmt.Errorf("%s contains a malformed git-a2a dependency region", rootFile)
	}
	blocks, _ := parseRegion(before)
	if _, exists := blocks[dep.Name]; !exists {
		return adapter.Change{File: rootFile, Entry: dep.Name}, nil
	}
	delete(blocks, dep.Name)
	after := removeRegion(before)
	if len(blocks) != 0 {
		after = upsertRegion(after, renderRegion(blocks))
	}
	if err = os.WriteFile(path, after, 0o644); err != nil {
		return adapter.Change{}, err
	}
	return adapter.Change{File: rootFile, Entry: dep.Name, Changed: true}, nil
}

func (Adapter) resolve(ctx context.Context, root string) error {
	build := filepath.Join(root, ".git-a2a", "build", "meson")
	if err := os.MkdirAll(filepath.Dir(build), 0o755); err != nil {
		return err
	}
	args := []string{"setup", build, root}
	if _, err := os.Stat(filepath.Join(build, "meson-private", "coredata.dat")); err == nil {
		args = []string{"setup", "--reconfigure", build, root}
	} else if !os.IsNotExist(err) {
		return err
	}
	return adapter.Command(ctx, root, "meson", args...)
}

func (Adapter) inspectDeclaration(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, _ adapter.Locked) ([]adapter.Finding, error) {
	if exp.Path == "" || exp.Path == "." {
		return []adapter.Finding{{File: rootFile, Entry: dep.Name, Want: "materialized source checkout path", Got: exp.Path}}, nil
	}
	body, err := os.ReadFile(filepath.Join(root, rootFile))
	if err != nil {
		return nil, err
	}
	if malformedRegion(body) {
		return nil, fmt.Errorf("%s contains a malformed git-a2a dependency region", rootFile)
	}
	blocks, foreign := parseRegion(body)
	var findings []adapter.Finding
	if got, want := blocks[dep.Name], sourcePath(exp); got != want {
		findings = append(findings, adapter.Finding{File: rootFile, Entry: dep.Name, Want: want, Got: got, Repairable: true})
	}
	if foreign {
		findings = append(findings, adapter.Finding{File: rootFile, Entry: "owned region", Want: "only generated git-a2a content", Got: "foreign content"})
	}
	return findings, nil
}

func sourcePath(exp adapter.Export) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(exp.Path)))
}

var (
	regionPattern = regexp.MustCompile(`(?ms)^# git-a2a:begin dependencies\n(.*?)^# git-a2a:end dependencies\n?`)
	linePattern   = regexp.MustCompile(`^subdir\('((?:[^'\\]|\\.)*)'\) # git-a2a: ([a-z0-9][a-z0-9._-]*)$`)
)

func parseRegion(body []byte) (map[string]string, bool) {
	blocks := map[string]string{}
	match := regionPattern.FindSubmatch(body)
	if match == nil {
		return blocks, false
	}
	foreign := false
	for _, line := range strings.Split(strings.TrimSuffix(string(match[1]), "\n"), "\n") {
		parsed := linePattern.FindStringSubmatch(line)
		if parsed == nil {
			if strings.TrimSpace(line) != "" {
				foreign = true
			}
			continue
		}
		blocks[parsed[2]] = strings.ReplaceAll(parsed[1], `\'`, `'`)
	}
	return blocks, foreign
}

func malformedRegion(body []byte) bool {
	text := string(body)
	hasMarker := strings.Contains(text, strings.TrimSuffix(regionBegin, "\n")) || strings.Contains(text, strings.TrimSuffix(regionEnd, "\n"))
	return hasMarker && !regionPattern.Match(body)
}

func renderRegion(blocks map[string]string) []byte {
	names := make([]string, 0, len(blocks))
	for name := range blocks {
		names = append(names, name)
	}
	sort.Strings(names)
	var out strings.Builder
	out.WriteString(regionBegin)
	for _, name := range names {
		fmt.Fprintf(&out, "subdir('%s') # git-a2a: %s\n", strings.ReplaceAll(blocks[name], `'`, `\'`), name)
	}
	out.WriteString(regionEnd)
	return []byte(out.String())
}

func removeRegion(body []byte) []byte { return regionPattern.ReplaceAll(body, nil) }

func upsertRegion(body, region []byte) []byte {
	if regionPattern.Match(body) {
		return regionPattern.ReplaceAllFunc(body, func([]byte) []byte { return region })
	}
	if projectEnd := mesonProjectEnd(body); projectEnd >= 0 {
		out := make([]byte, 0, len(body)+len(region))
		out = append(out, body[:projectEnd]...)
		out = append(out, region...)
		out = append(out, body[projectEnd:]...)
		return out
	}
	out := append([]byte(nil), body...)
	if len(out) != 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return append(out, region...)
}

// mesonProjectEnd returns the byte immediately after the project() statement's
// terminating newline. Meson evaluates variables sequentially, so dependency
// subdir() calls must precede the consumer statements that use their variables.
func mesonProjectEnd(body []byte) int {
	start := regexp.MustCompile(`(?m)^project\s*\(`).FindIndex(body)
	if start == nil {
		return -1
	}
	depth := 0
	var quote byte
	escaped := false
	for i := start[0]; i < len(body); i++ {
		c := body[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				if i+1 < len(body) && body[i+1] == '\n' {
					return i + 2
				}
				return i + 1
			}
		}
	}
	return -1
}
