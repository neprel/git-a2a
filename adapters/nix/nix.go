package nix

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/neprel/git-a2a/internal/adapter"
	"github.com/neprel/git-a2a/internal/gitx"
)

type Adapter struct{}

func (Adapter) Ecosystem() string { return "nix" }

func (Adapter) Detect(root string) (bool, adapter.Variant, error) {
	_, err := os.Stat(filepath.Join(root, "flake.nix"))
	if os.IsNotExist(err) {
		return false, "", nil
	}
	return err == nil, "flake", err
}

func (Adapter) Wire(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	path := filepath.Join(root, "flake.nix")
	body, err := os.ReadFile(path)
	if err != nil {
		return adapter.Change{}, err
	}
	inputURL, err := flakeURL(dep, exp, locked)
	if err != nil {
		return adapter.Change{}, adapter.NotWirable(err.Error())
	}
	block := "  # git-a2a:begin " + exp.Name + "\n  inputs." + attrName(exp.Name) + ".url = " + strconv.Quote(inputURL) + ";\n  # git-a2a:end " + exp.Name + "\n"
	next, changed, err := upsert(string(body), exp.Name, block)
	if err == nil && changed {
		err = os.WriteFile(path, []byte(next), 0o644)
	}
	return adapter.Change{File: "flake.nix", Entry: exp.Name, Changed: changed}, err
}

func (Adapter) Unwire(_ context.Context, root string, _ adapter.Dependency, exp adapter.Export) (adapter.Change, error) {
	path := filepath.Join(root, "flake.nix")
	body, err := os.ReadFile(path)
	if err != nil {
		return adapter.Change{}, err
	}
	re := managedBlock(exp.Name)
	if !re.Match(body) {
		return adapter.Change{File: "flake.nix", Entry: exp.Name}, nil
	}
	next := re.ReplaceAllString(string(body), "")
	err = os.WriteFile(path, []byte(next), 0o644)
	return adapter.Change{File: "flake.nix", Entry: exp.Name, Changed: true}, err
}

func (Adapter) Refresh(ctx context.Context, _ string, _ adapter.Dependency, _ adapter.Export, _ adapter.Locked) error {
	return adapter.RequireTool(ctx, "nix", "flake")
}

func (Adapter) Drift(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	body, err := os.ReadFile(filepath.Join(root, "flake.nix"))
	if err != nil {
		return nil, err
	}
	block := managedBlock(exp.Name).FindString(string(body))
	match := regexp.MustCompile(`\.url\s*=\s*"([^"]+)"`).FindStringSubmatch(block)
	got := ""
	if len(match) == 2 {
		got = match[1]
	}
	parsed, _ := url.Parse(got)
	base := got
	if parsed != nil && parsed.Scheme != "" {
		parsed.RawQuery = ""
		base = parsed.String()
	}
	base = strings.TrimPrefix(base, "git+")
	badPin := dep.Track != "floating" && !strings.Contains(got, "rev="+locked.Commit)
	if block == "" || gitx.NormalizeURL(base) != gitx.NormalizeURL(locked.Git) || badPin {
		return []adapter.Finding{{File: "flake.nix", Entry: exp.Name, Want: locked.Commit, Got: strings.TrimSpace(block)}}, nil
	}
	return nil, nil
}

func flakeURL(dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (string, error) {
	value := dep.Git
	if strings.HasPrefix(value, "git@") {
		parts := strings.SplitN(value, ":", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("invalid SSH Git URL")
		}
		value = "ssh://" + parts[0] + "/" + parts[1]
	}
	if !strings.HasPrefix(value, "git+") {
		value = "git+" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return "", fmt.Errorf("invalid Git URL")
	}
	query := parsed.Query()
	if dep.Ref != "" {
		query.Set("ref", strings.TrimPrefix(dep.Ref, "refs/heads/"))
	}
	if dep.Track != "floating" {
		query.Set("rev", locked.Commit)
	}
	if exp.Path != "" && exp.Path != "." {
		query.Set("dir", exp.Path)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func upsert(document, name, block string) (string, bool, error) {
	re := managedBlock(name)
	if old := re.FindString(document); old != "" {
		if old == block {
			return document, false, nil
		}
		return re.ReplaceAllStringFunc(document, func(string) string { return block }), true, nil
	}
	start := strings.Index(document, "{")
	if start < 0 {
		return "", false, fmt.Errorf("flake.nix: top-level attribute set is required")
	}
	end, ok := matchingBrace(document, start)
	if !ok {
		return "", false, fmt.Errorf("flake.nix: unterminated top-level attribute set")
	}
	insertAt := strings.LastIndex(document[:end], "\n") + 1
	return document[:insertAt] + block + document[insertAt:], true, nil
}

func matchingBrace(document string, start int) (int, bool) {
	depth, inString, escape, comment := 0, false, false, false
	for i := start; i < len(document); i++ {
		c := document[i]
		if comment {
			if c == '\n' {
				comment = false
			}
			continue
		}
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
		if c == '#' {
			comment = true
		} else if c == '"' {
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
	return regexp.MustCompile(`(?ms)^[ \t]*# git-a2a:begin ` + regexp.QuoteMeta(name) + `\n.*?^[ \t]*# git-a2a:end ` + regexp.QuoteMeta(name) + `\n`)
}

func attrName(name string) string {
	if regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_'-]*$`).MatchString(name) {
		return name
	}
	return strconv.Quote(name)
}
