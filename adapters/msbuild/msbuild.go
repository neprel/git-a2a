package msbuild

import (
	"context"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/neprel/git-a2a/internal/adapter"
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

func (a Adapter) Wire(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	if dep.Vendor == nil || locked.Vendor == nil {
		return adapter.Change{}, adapter.NotWirable("MSBuild project integration requires an explicitly vendored dependency")
	}
	project, err := consumerProject(root)
	if err != nil || project == "" {
		return adapter.Change{}, err
	}
	reference, err := vendoredProject(root, dep, exp, locked)
	if err != nil {
		return adapter.Change{}, err
	}
	generatedPath := filepath.Join(root, filepath.FromSlash(generatedFile))
	before, err := readFile(generatedPath)
	if err != nil {
		return adapter.Change{}, err
	}
	blocks, discarded := parseBlocks(before)
	blocks[dep.ID] = block(dep.ID, reference)
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
		return adapter.Change{File: generatedFile, Entry: dep.ID}, nil
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
	return adapter.Change{File: generatedFile, Entry: dep.ID, Changed: true, Warning: warning}, err
}

func (Adapter) Unwire(_ context.Context, root string, dep adapter.Dependency, _ adapter.Export) (adapter.Change, error) {
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
	_, hadBlock := blocks[dep.ID]
	delete(blocks, dep.ID)
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
	return adapter.Change{File: generatedFile, Entry: dep.ID, Changed: hadBlock || discarded || len(before) > 0}, err
}

func (a Adapter) Refresh(ctx context.Context, root string, _ adapter.Dependency, _ adapter.Export, _ adapter.Locked) error {
	project, err := consumerProject(root)
	if err != nil || project == "" {
		return err
	}
	_, variant, _ := a.Detect(root)
	if err = adapter.RequireTool(ctx, a.Ecosystem(), variant); err != nil {
		return err
	}
	return adapter.Command(ctx, root, "dotnet", "build", project, "--nologo")
}

func (Adapter) Drift(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	if dep.Vendor == nil || locked.Vendor == nil {
		return []adapter.Finding{{File: generatedFile, Entry: dep.ID, Want: "vendored MSBuild integration", Got: "not vendored"}}, nil
	}
	project, err := consumerProject(root)
	if err != nil || project == "" {
		return nil, err
	}
	reference, err := vendoredProject(root, dep, exp, locked)
	if err != nil {
		return nil, err
	}
	body, err := readFile(filepath.Join(root, filepath.FromSlash(generatedFile)))
	if err != nil {
		return nil, err
	}
	blocks, discarded := parseBlocks(body)
	want := block(dep.ID, reference)
	var findings []adapter.Finding
	if blocks[dep.ID] != want {
		findings = append(findings, adapter.Finding{File: generatedFile, Entry: dep.ID, Want: strings.TrimSpace(want), Got: strings.TrimSpace(blocks[dep.ID])})
	}
	if discarded {
		findings = append(findings, adapter.Finding{File: generatedFile, Entry: "owned file", Want: "only generated git-a2a content", Got: "foreign content"})
	}
	projectBody, err := os.ReadFile(filepath.Join(root, project))
	if err != nil {
		return nil, err
	}
	if !hasImport(projectBody) {
		findings = append(findings, adapter.Finding{File: project, Entry: "git-a2a import", Want: importLine, Got: ""})
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

func vendoredProject(root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (string, error) {
	parts := []string{locked.Vendor.Path}
	if locked.Vendor.Mode == "submodule" && locked.Path != "" && locked.Path != "." {
		parts = append(parts, locked.Path)
	}
	if exp.Path != "" && exp.Path != "." {
		parts = append(parts, exp.Path)
	}
	rel := filepath.Join(parts...)
	if strings.HasSuffix(rel, ".csproj") || strings.HasSuffix(rel, ".fsproj") {
		return filepath.ToSlash(rel), nil
	}
	abs := filepath.Join(root, rel)
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("nuget export %s: vendored project path: %w", exp.Name, err)
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
