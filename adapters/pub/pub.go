package pub

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

func (Adapter) Ecosystem() string { return "pub" }
func (Adapter) Detect(root string) (bool, adapter.Variant, error) {
	_, err := os.Stat(filepath.Join(root, "pubspec.yaml"))
	if os.IsNotExist(err) {
		return false, "", nil
	}
	return err == nil, "pub", err
}

func (Adapter) Wire(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	file := filepath.Join(root, "pubspec.yaml")
	body, err := os.ReadFile(file)
	if err != nil {
		return adapter.Change{}, err
	}
	lines := []string{fmt.Sprintf("  %s:", exp.Name)}
	if locked.Vendor != nil {
		lines = append(lines, "    path: "+strconv.Quote(adapter.VendorSourcePath(exp, locked)))
	} else {
		ref := locked.Commit
		if dep.Track == "floating" {
			ref = dep.Ref
		}
		lines = append(lines, "    git:", "      url: "+strconv.Quote(dep.Git), "      ref: "+strconv.Quote(ref))
		if exp.Path != "" && exp.Path != "." {
			lines = append(lines, "      path: "+strconv.Quote(strings.Trim(exp.Path, "/")))
		}
	}
	entry := strings.Join(lines, "\n") + "\n"
	next, changed, err := upsert(string(body), exp.Name, entry)
	if err != nil {
		return adapter.Change{}, adapter.NotWirable(err.Error())
	}
	if changed {
		err = os.WriteFile(file, []byte(next), 0o644)
	}
	return adapter.Change{File: "pubspec.yaml", Entry: exp.Name, Changed: changed}, err
}

func (Adapter) Unwire(_ context.Context, root string, _ adapter.Dependency, exp adapter.Export) (adapter.Change, error) {
	file := filepath.Join(root, "pubspec.yaml")
	body, err := os.ReadFile(file)
	if err != nil {
		return adapter.Change{}, err
	}
	if re := createdDependencies(exp.Name); re.Match(body) {
		next := re.ReplaceAll(body, nil)
		err = os.WriteFile(file, next, 0o644)
		return adapter.Change{File: "pubspec.yaml", Entry: exp.Name, Changed: true}, err
	}
	start, end, ok, rangeErr := entryRange(string(body), exp.Name)
	if rangeErr != nil || !ok {
		return adapter.Change{File: "pubspec.yaml", Entry: exp.Name}, rangeErr
	}
	next := string(body[:start]) + string(body[end:])
	err = os.WriteFile(file, []byte(next), 0o644)
	return adapter.Change{File: "pubspec.yaml", Entry: exp.Name, Changed: true}, err
}

func (Adapter) Refresh(ctx context.Context, _ string, _ adapter.Dependency, _ adapter.Export, _ adapter.Locked) error {
	return adapter.RequireTool(ctx, "pub", "pub")
}

func (Adapter) Drift(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	body, err := os.ReadFile(filepath.Join(root, "pubspec.yaml"))
	if err != nil {
		return nil, err
	}
	start, end, ok, _ := entryRange(string(body), exp.Name)
	entry := ""
	if ok {
		entry = string(body[start:end])
	}
	if locked.Vendor != nil {
		want := "path: " + strconv.Quote(adapter.VendorSourcePath(exp, locked))
		if entry == "" || !strings.Contains(entry, want) {
			return []adapter.Finding{{File: "pubspec.yaml", Entry: exp.Name, Want: want, Got: strings.TrimSpace(entry)}}, nil
		}
		return nil, nil
	}
	urlMatch := regexp.MustCompile(`(?m)^\s+url:\s*["']?([^"'\s]+)`).FindStringSubmatch(entry)
	gotURL := ""
	if len(urlMatch) == 2 {
		gotURL = urlMatch[1]
	}
	badURL := gotURL == "" || gitx.NormalizeURL(gotURL) != gitx.NormalizeURL(locked.Git)
	badPin := dep.Track != "floating" && !strings.Contains(entry, locked.Commit)
	if entry == "" || badURL || badPin {
		return []adapter.Finding{{File: "pubspec.yaml", Entry: exp.Name, Want: locked.Commit, Got: strings.TrimSpace(entry)}}, nil
	}
	return nil, nil
}

func upsert(document, name, entry string) (string, bool, error) {
	_, sectionEnd, ok := dependenciesRange(document)
	if !ok {
		separator := "\n"
		if strings.HasSuffix(document, "\n") {
			separator = ""
		}
		block := "# git-a2a:begin " + name + "\ndependencies:\n" + entry + "# git-a2a:end " + name + "\n"
		return document + separator + block, true, nil
	}
	start, end, found, err := entryRange(document, name)
	if err != nil {
		return "", false, err
	}
	if found {
		if document[start:end] == entry {
			return document, false, nil
		}
		return document[:start] + entry + document[end:], true, nil
	}
	return document[:sectionEnd] + entry + document[sectionEnd:], true, nil
}

func createdDependencies(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?ms)^# git-a2a:begin ` + regexp.QuoteMeta(name) + `\ndependencies:\n.*?^# git-a2a:end ` + regexp.QuoteMeta(name) + `\n`)
}

func dependenciesRange(document string) (int, int, bool) {
	header := regexp.MustCompile(`(?m)^dependencies:[ \t]*(?:#.*)?$`).FindStringIndex(document)
	if header == nil {
		return 0, 0, false
	}
	start := header[1]
	if start < len(document) && document[start] == '\n' {
		start++
	}
	end := len(document)
	if next := regexp.MustCompile(`(?m)^(?:[A-Za-z_][A-Za-z0-9_-]*:|# git-a2a:end )`).FindStringIndex(document[start:]); next != nil {
		end = start + next[0]
	}
	return start, end, true
}

func entryRange(document, name string) (int, int, bool, error) {
	sectionStart, sectionEnd, ok := dependenciesRange(document)
	if !ok {
		return 0, 0, false, nil
	}
	section := document[sectionStart:sectionEnd]
	if strings.TrimSpace(section) != "" && !regexp.MustCompile(`(?m)^  [A-Za-z_]`).MatchString(section) {
		return 0, 0, false, fmt.Errorf("pubspec.yaml: dependencies must be a block mapping")
	}
	key := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(name) + `:[^\n]*(?:\n|$)`).FindStringIndex(section)
	if key == nil {
		return 0, 0, false, nil
	}
	start := sectionStart + key[0]
	end := sectionEnd
	if next := regexp.MustCompile(`(?m)^  [A-Za-z_][A-Za-z0-9_]*:`).FindStringIndex(document[sectionStart+key[1] : sectionEnd]); next != nil {
		end = sectionStart + key[1] + next[0]
	}
	return start, end, true, nil
}
