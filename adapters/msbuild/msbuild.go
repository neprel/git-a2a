package msbuild

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/neprel/git-a2a/v2/internal/adapter"
)

const (
	generatedFile = "deps/git-a2a.targets"
	header        = "<Project>\n"
	footer        = "</Project>\n"
	importLine    = `<Import Project="deps/git-a2a.targets" Label="git-a2a" />`
)

type Adapter struct{}

func (Adapter) Ecosystem() string { return "nuget" }

func (Adapter) Detect(root string) (bool, adapter.Variant, error) {
	project, err := consumerProject(root)
	if err != nil || project == "" {
		return false, "", err
	}
	if strings.HasSuffix(project, ".fsproj") {
		return true, "msbuild-fsharp", nil
	}
	return true, "msbuild-csharp", nil
}

func (a Adapter) wire(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, _ adapter.Locked) (adapter.Change, error) {
	if exp.Path == "" || exp.Path == "." {
		return adapter.Change{}, adapter.NotWirable("MSBuild project integration requires a materialized source checkout path")
	}
	project, err := consumerProject(root)
	if err != nil || project == "" {
		return adapter.Change{}, err
	}
	reference, err := checkoutProject(root, exp)
	if err != nil {
		return adapter.Change{}, err
	}
	generatedPath := filepath.Join(root, filepath.FromSlash(generatedFile))
	before, err := readFile(generatedPath)
	if err != nil {
		return adapter.Change{}, err
	}
	blocks, discarded := parseBlocks(before)
	blocks[dep.Name] = block(dep.Name, reference)
	next := renderBlocks(blocks)
	projectPath := filepath.Join(root, project)
	projectBefore, err := os.ReadFile(projectPath)
	if err != nil {
		return adapter.Change{}, err
	}
	projectAfter, err := ensureImport(projectBefore)
	if err != nil {
		return adapter.Change{}, err
	}
	changed := string(before) != string(next) || string(projectBefore) != string(projectAfter)
	if !changed {
		return adapter.Change{File: generatedFile, Entry: dep.Name}, nil
	}
	if err = os.MkdirAll(filepath.Dir(generatedPath), 0o755); err == nil {
		err = os.WriteFile(generatedPath, next, 0o644)
	}
	if err == nil {
		err = os.WriteFile(projectPath, projectAfter, 0o644)
	}
	warning := ""
	if discarded {
		warning = generatedFile + " contained foreign content; git-a2a regenerated the owned file and discarded it"
	}
	return adapter.Change{File: generatedFile, Entry: dep.Name, Changed: true, Warning: warning}, err
}

func (Adapter) unwire(_ context.Context, root string, dep adapter.Dependency, _ adapter.Export) (adapter.Change, error) {
	project, err := consumerProject(root)
	if err != nil || project == "" {
		return adapter.Change{}, err
	}
	generatedPath := filepath.Join(root, filepath.FromSlash(generatedFile))
	before, err := readFile(generatedPath)
	if err != nil {
		return adapter.Change{}, err
	}
	blocks, discarded := parseBlocks(before)
	_, hadBlock := blocks[dep.Name]
	delete(blocks, dep.Name)
	if len(blocks) == 0 {
		if err = os.Remove(generatedPath); err != nil && !os.IsNotExist(err) {
			return adapter.Change{}, err
		}
		projectPath := filepath.Join(root, project)
		body, readErr := os.ReadFile(projectPath)
		if readErr != nil {
			return adapter.Change{}, readErr
		}
		err = os.WriteFile(projectPath, removeImport(body), 0o644)
	} else {
		err = os.WriteFile(generatedPath, renderBlocks(blocks), 0o644)
	}
	return adapter.Change{File: generatedFile, Entry: dep.Name, Changed: hadBlock || discarded || len(before) > 0}, err
}

func (a Adapter) resolve(ctx context.Context, root string) error {
	project, err := consumerProject(root)
	if err != nil || project == "" {
		return err
	}
	return adapter.Command(ctx, root, "dotnet", "restore", project, "--nologo")
}

func (Adapter) inspectDeclaration(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, _ adapter.Locked) ([]adapter.Finding, error) {
	if exp.Path == "" || exp.Path == "." {
		return []adapter.Finding{{File: generatedFile, Entry: dep.Name, Want: "materialized source checkout path", Got: exp.Path}}, nil
	}
	project, err := consumerProject(root)
	if err != nil || project == "" {
		return nil, err
	}
	body, err := readFile(filepath.Join(root, filepath.FromSlash(generatedFile)))
	if err != nil {
		return nil, err
	}
	blocks, discarded := parseBlocks(body)
	var findings []adapter.Finding
	reference, checkoutErr := checkoutProject(root, exp)
	if checkoutErr != nil {
		if blocks[dep.Name] == "" {
			findings = append(findings, adapter.Finding{File: generatedFile, Entry: dep.Name, Want: "generated project reference", Got: "", Repairable: true})
		}
		findings = append(findings, adapter.Finding{File: exp.Path, Entry: dep.Name, Want: "materialized MSBuild project", Got: checkoutErr.Error(), Repairable: errors.Is(checkoutErr, os.ErrNotExist)})
	} else {
		want := block(dep.Name, reference)
		if blocks[dep.Name] != want {
			findings = append(findings, adapter.Finding{File: generatedFile, Entry: dep.Name, Want: strings.TrimSpace(want), Got: strings.TrimSpace(blocks[dep.Name]), Repairable: true})
		}
	}
	if discarded {
		findings = append(findings, adapter.Finding{File: generatedFile, Entry: "owned file", Want: "only generated git-a2a content", Got: "foreign content"})
	}
	projectBody, err := os.ReadFile(filepath.Join(root, project))
	if err != nil {
		return nil, err
	}
	if !hasImport(projectBody) {
		findings = append(findings, adapter.Finding{File: project, Entry: "git-a2a import", Want: importLine, Got: "", Repairable: true})
	}
	return findings, nil
}

func consumerProject(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	var projects []string
	for _, entry := range entries {
		if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".csproj") || strings.HasSuffix(entry.Name(), ".fsproj")) {
			projects = append(projects, entry.Name())
		}
	}
	sort.Strings(projects)
	if len(projects) == 0 {
		return "", nil
	}
	return projects[0], nil
}

func checkoutProject(root string, exp adapter.Export) (string, error) {
	rel := filepath.Clean(filepath.FromSlash(exp.Path))
	if strings.HasSuffix(rel, ".csproj") || strings.HasSuffix(rel, ".fsproj") {
		info, err := os.Stat(filepath.Join(root, rel))
		if err != nil {
			return "", fmt.Errorf("nuget export %s: checkout project path: %w", exp.Name, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("nuget export %s: %s is not an MSBuild project file", exp.Name, filepath.ToSlash(rel))
		}
		return filepath.ToSlash(rel), nil
	}
	abs := filepath.Join(root, rel)
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("nuget export %s: checkout project path: %w", exp.Name, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("nuget export %s: %s is not an MSBuild project", exp.Name, filepath.ToSlash(rel))
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return "", err
	}
	var matches []string
	for _, entry := range entries {
		if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".csproj") || strings.HasSuffix(entry.Name(), ".fsproj")) {
			matches = append(matches, entry.Name())
		}
	}
	sort.Strings(matches)
	if len(matches) != 1 {
		return "", fmt.Errorf("nuget export %s: %s must contain exactly one .csproj or .fsproj", exp.Name, filepath.ToSlash(rel))
	}
	return filepath.ToSlash(filepath.Join(rel, matches[0])), nil
}

func block(id, project string) string {
	directory := filepath.ToSlash(filepath.Dir(project))
	remove := directory + "/**/*.cs;" + directory + "/**/*.fs"
	return fmt.Sprintf("  <!-- git-a2a:begin %s -->\n  <ItemGroup>\n    <Compile Remove=\"%s\" />\n    <ProjectReference Include=\"%s\" />\n  </ItemGroup>\n  <!-- git-a2a:end %s -->\n", id, html.EscapeString(remove), html.EscapeString(project), id)
}

var blockPattern = regexp.MustCompile(`(?ms)^  <!-- git-a2a:begin ([a-z0-9][a-z0-9._-]*) -->\n.*?^  <!-- git-a2a:end ([a-z0-9][a-z0-9._-]*) -->\n?`)

func readFile(path string) ([]byte, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return body, err
}

func parseBlocks(body []byte) (map[string]string, bool) {
	blocks := map[string]string{}
	if len(body) == 0 {
		return blocks, false
	}
	text := string(body)
	hasFrame := strings.HasPrefix(text, header) && strings.HasSuffix(text, footer)
	remainder := strings.TrimPrefix(text, header)
	remainder = strings.TrimSuffix(remainder, footer)
	for _, match := range blockPattern.FindAllStringSubmatch(text, -1) {
		if match[1] != match[2] {
			continue
		}
		blocks[match[1]] = match[0]
		remainder = strings.Replace(remainder, match[0], "", 1)
	}
	return blocks, !hasFrame || strings.TrimSpace(remainder) != ""
}

func renderBlocks(blocks map[string]string) []byte {
	ids := make([]string, 0, len(blocks))
	for id := range blocks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out strings.Builder
	out.WriteString(header)
	for _, id := range ids {
		out.WriteString(blocks[id])
	}
	out.WriteString(footer)
	return []byte(out.String())
}

func hasImport(body []byte) bool {
	for _, line := range strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == importLine {
			return true
		}
	}
	return false
}

func ensureImport(body []byte) ([]byte, error) {
	if hasImport(body) {
		return body, nil
	}
	newline := "\n"
	if strings.Contains(string(body), "\r\n") {
		newline = "\r\n"
	}
	closing := []byte("</Project>")
	index := strings.LastIndex(string(body), string(closing))
	if index < 0 {
		return nil, fmt.Errorf("consumer MSBuild project has no closing </Project>")
	}
	prefix := string(body[:index])
	if prefix != "" && !strings.HasSuffix(prefix, "\n") {
		prefix += newline
	}
	return []byte(prefix + "  " + importLine + newline + string(body[index:])), nil
}

func removeImport(body []byte) []byte {
	lines := strings.SplitAfter(string(body), "\n")
	out := lines[:0]
	for _, line := range lines {
		if strings.TrimSpace(line) != importLine {
			out = append(out, line)
		}
	}
	return []byte(strings.Join(out, ""))
}
