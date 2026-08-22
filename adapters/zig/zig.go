package zig

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

func (Adapter) Ecosystem() string { return "zig" }

func (Adapter) Detect(root string) (bool, adapter.Variant, error) {
	_, err := os.Stat(filepath.Join(root, "build.zig.zon"))
	if os.IsNotExist(err) {
		return false, "", nil
	}
	return err == nil, "zon", err
}

func (Adapter) Wire(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	if exp.Path != "" && exp.Path != "." {
		return adapter.Change{}, adapter.NotWirable("Zig package URLs cannot select a repository subdirectory")
	}
	hash, _ := exp.Extensions["x-zig-hash"].(string)
	if strings.TrimSpace(hash) == "" {
		return adapter.Change{}, adapter.NotWirable("Zig requires exports[].x-zig-hash for package integrity")
	}
	path := filepath.Join(root, "build.zig.zon")
	body, err := os.ReadFile(path)
	if err != nil {
		return adapter.Change{}, err
	}
	ref := locked.Commit
	if dep.Track == "floating" {
		ref = dep.Ref
	}
	block := fmt.Sprintf("    // git-a2a:begin %s\n    %s = .{\n      .url = %s,\n      .hash = %s,\n    },\n    // git-a2a:end %s\n", exp.Name, fieldName(exp.Name), strconv.Quote(gitURL(dep.Git, ref)), strconv.Quote(hash), exp.Name)
	next, changed, err := upsert(string(body), exp.Name, block)
	if err == nil && changed {
		err = os.WriteFile(path, []byte(next), 0o644)
	}
	return adapter.Change{File: "build.zig.zon", Entry: exp.Name, Changed: changed}, err
}

func (Adapter) Unwire(_ context.Context, root string, _ adapter.Dependency, exp adapter.Export) (adapter.Change, error) {
	path := filepath.Join(root, "build.zig.zon")
	body, err := os.ReadFile(path)
	if err != nil {
		return adapter.Change{}, err
	}
	re := managedBlock(exp.Name)
	if created := createdDependencies(exp.Name); created.Match(body) {
		next := created.ReplaceAll(body, nil)
		err = os.WriteFile(path, next, 0o644)
		return adapter.Change{File: "build.zig.zon", Entry: exp.Name, Changed: true}, err
	}
	if !re.Match(body) {
		return adapter.Change{File: "build.zig.zon", Entry: exp.Name}, nil
	}
	next := re.ReplaceAllString(string(body), "")
	err = os.WriteFile(path, []byte(next), 0o644)
	return adapter.Change{File: "build.zig.zon", Entry: exp.Name, Changed: true}, err
}

func (Adapter) Refresh(ctx context.Context, _ string, _ adapter.Dependency, _ adapter.Export, _ adapter.Locked) error {
	return adapter.RequireTool(ctx, "zig", "zon")
}

func (Adapter) Drift(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	body, err := os.ReadFile(filepath.Join(root, "build.zig.zon"))
	if err != nil {
		return nil, err
	}
	block := managedBlock(exp.Name).FindString(string(body))
	match := regexp.MustCompile(`\.url\s*=\s*"([^"]+)"`).FindStringSubmatch(block)
	gotURL := ""
	if len(match) == 2 {
		gotURL = match[1]
	}
	base := strings.TrimPrefix(gotURL, "git+")
	if at := strings.LastIndex(base, "#"); at >= 0 {
		base = base[:at]
	}
	badPin := dep.Track != "floating" && !strings.Contains(gotURL, locked.Commit)
	if block == "" || gitx.NormalizeURL(base) != gitx.NormalizeURL(locked.Git) || badPin {
		return []adapter.Finding{{File: "build.zig.zon", Entry: exp.Name, Want: locked.Commit, Got: strings.TrimSpace(block)}}, nil
	}
	return nil, nil
}

func upsert(document, name, block string) (string, bool, error) {
	re := managedBlock(name)
	if old := re.FindString(document); old != "" {
		if old == block {
			return document, false, nil
		}
		return re.ReplaceAllStringFunc(document, func(string) string { return block }), true, nil
	}
	start, end, ok := dependenciesObject(document)
	if !ok {
		rootStart, rootEnd, rootOK := rootObject(document)
		if !rootOK {
			return "", false, fmt.Errorf("build.zig.zon: top-level object is required")
		}
		_ = rootStart
		insertAt := strings.LastIndex(document[:rootEnd], "\n") + 1
		container := "    // git-a2a:begin-container " + name + "\n    .dependencies = .{\n" + block + "    },\n    // git-a2a:end-container " + name + "\n"
		return document[:insertAt] + container + document[insertAt:], true, nil
	}
	_ = start
	insertAt := strings.LastIndex(document[:end], "\n") + 1
	return document[:insertAt] + block + document[insertAt:], true, nil
}

func rootObject(document string) (int, int, bool) {
	loc := regexp.MustCompile(`\.\s*\{`).FindStringIndex(document)
	if loc == nil {
		return 0, 0, false
	}
	start := loc[1] - 1
	end, ok := matchingBrace(document, start)
	return start, end, ok
}

func createdDependencies(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?ms)^[ \t]*// git-a2a:begin-container ` + regexp.QuoteMeta(name) + `\n.*?^[ \t]*// git-a2a:end-container ` + regexp.QuoteMeta(name) + `\n`)
}

func dependenciesObject(document string) (int, int, bool) {
	loc := regexp.MustCompile(`(?m)\.dependencies\s*=\s*\.\{`).FindStringIndex(document)
	if loc == nil {
		return 0, 0, false
	}
	start := strings.LastIndex(document[:loc[1]], "{")
	if start < 0 {
		return 0, 0, false
	}
	end, ok := matchingBrace(document, start)
	return start, end, ok
}

func matchingBrace(document string, start int) (int, bool) {
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
		} else if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

func managedBlock(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?ms)^[ \t]*// git-a2a:begin ` + regexp.QuoteMeta(name) + `\n.*?^[ \t]*// git-a2a:end ` + regexp.QuoteMeta(name) + `\n`)
}

func fieldName(name string) string {
	if regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(name) {
		return "." + name
	}
	return ".@" + strconv.Quote(name)
}

func gitURL(value, ref string) string {
	if strings.HasPrefix(value, "git@") {
		parts := strings.SplitN(value, ":", 2)
		if len(parts) == 2 {
			value = "ssh://" + parts[0] + "/" + parts[1]
		}
	}
	if !strings.HasPrefix(value, "git+") {
		value = "git+" + value
	}
	return value + "#" + ref
}
