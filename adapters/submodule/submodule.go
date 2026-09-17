package submodule

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/neprel/git-a2a/internal/adapter"
)

type Adapter struct{}

func (a Adapter) Capability(root string, _ adapter.Dependency, _ adapter.Export) error {
	ok, _, err := a.Detect(root)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("submodule consumer must be a Git repository")
	}
	return nil
}

func (a Adapter) Pull(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	return a.Wire(ctx, root, dep, exp, locked)
}

func (a Adapter) Remove(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, _ adapter.Locked) (adapter.Change, error) {
	return a.Unwire(ctx, root, dep, exp)
}

func (a Adapter) Inspect(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	return a.Drift(ctx, root, dep, exp, locked)
}

func (Adapter) Ecosystem() string { return "submodule" }

func (Adapter) Detect(root string) (bool, adapter.Variant, error) {
	ctx := context.Background()
	if err := adapter.RequireTool(ctx, "git", ""); err != nil {
		return false, "", err
	}
	_, err := adapter.CommandOutput(ctx, root, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return false, "", nil
	}
	return true, "git", nil
}

func (a Adapter) Wire(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	return a.apply(ctx, root, dep, exp, locked)
}

func (Adapter) Unwire(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export) (adapter.Change, error) {
	relative, err := bindingPath(root, dep, exp)
	if err != nil {
		return adapter.Change{}, err
	}
	if err = requireRepository(ctx, root); err != nil {
		return adapter.Change{}, err
	}
	if err = rejectConflicts(ctx, root, relative); err != nil {
		return adapter.Change{}, err
	}
	if err = validateGitmodules(ctx, root); err != nil {
		return adapter.Change{}, err
	}
	entry, err := readIndexEntry(ctx, root, relative)
	if err != nil {
		return adapter.Change{}, err
	}
	dest := filepath.Join(root, filepath.FromSlash(relative))
	if entry == nil {
		if _, statErr := os.Lstat(dest); statErr == nil {
			return adapter.Change{}, fmt.Errorf("submodule %q is not registered as a gitlink; refusing to remove the existing path", relative)
		} else if !os.IsNotExist(statErr) {
			return adapter.Change{}, statErr
		}
		return adapter.Change{File: relative, Entry: dep.Name}, nil
	}
	if entry.mode != "160000" {
		return adapter.Change{}, fmt.Errorf("submodule path %q is tracked with mode %s, not as a gitlink", relative, entry.mode)
	}
	section, source, err := registeredSource(root, relative)
	if err != nil {
		return adapter.Change{}, err
	}
	if section == "" {
		return adapter.Change{}, fmt.Errorf("submodule path %q has no matching .gitmodules registration", relative)
	}
	if dep.Git != "" && source != dep.Git {
		return adapter.Change{}, fmt.Errorf("submodule path %q belongs to %q, not %q", relative, source, dep.Git)
	}
	if err = requireCleanCheckout(ctx, dest, relative); err != nil {
		return adapter.Change{}, err
	}

	gitmodules := filepath.Join(root, ".gitmodules")
	original, readErr := os.ReadFile(gitmodules)
	hadGitmodules := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return adapter.Change{}, readErr
	}
	if err = removeSection(gitmodules, section); err != nil {
		return adapter.Change{}, err
	}
	if body, readBodyErr := os.ReadFile(gitmodules); readBodyErr == nil && len(bytes.TrimSpace(body)) == 0 {
		if err = os.Remove(gitmodules); err != nil {
			return adapter.Change{}, err
		}
	}
	// Git refuses to remove a gitlink while .gitmodules has an unstaged
	// matching edit. Stage the narrowed registration update first; every error
	// path below restores both the worktree bytes and their index entry.
	if err = adapter.Command(ctx, root, "git", "add", "-A", "--", ".gitmodules"); err != nil {
		restoreFile(gitmodules, original, hadGitmodules)
		_ = adapter.Command(ctx, root, "git", "add", "-A", "--", ".gitmodules")
		return adapter.Change{}, err
	}
	if err = adapter.Command(ctx, root, "git", "rm", "--cached", "--", filepath.FromSlash(relative)); err != nil {
		restoreFile(gitmodules, original, hadGitmodules)
		_ = adapter.Command(ctx, root, "git", "add", "-A", "--", ".gitmodules")
		return adapter.Change{}, err
	}
	if err = os.RemoveAll(dest); err != nil {
		return adapter.Change{}, fmt.Errorf("remove submodule worktree %q: %w", relative, err)
	}
	removeLocalConfigSection(ctx, root, section)
	removeModuleStore(ctx, root, relative)
	return adapter.Change{File: relative, Entry: dep.Name, Changed: true}, nil
}

func (Adapter) Drift(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	relative, err := bindingPath(root, dep, exp)
	if err != nil {
		return nil, err
	}
	if err = requireRepository(ctx, root); err != nil {
		return nil, err
	}
	if err = rejectConflicts(ctx, root, relative); err != nil {
		return []adapter.Finding{{File: relative, Entry: dep.Name, Want: "resolved index", Got: "unmerged"}}, nil
	}
	if err = validateGitmodules(ctx, root); err != nil {
		return []adapter.Finding{{File: ".gitmodules", Entry: relative, Want: "safe regular file", Got: err.Error()}}, nil
	}
	entry, err := readIndexEntry(ctx, root, relative)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return []adapter.Finding{{File: relative, Entry: dep.Name, Want: "160000 " + locked.Commit, Got: "missing"}}, nil
	}
	var findings []adapter.Finding
	if entry.mode != "160000" || entry.oid != locked.Commit {
		findings = append(findings, adapter.Finding{File: relative, Entry: dep.Name, Want: "160000 " + locked.Commit, Got: entry.mode + " " + entry.oid, Repairable: entry.mode == "160000"})
	}
	_, source, sourceErr := registeredSource(root, relative)
	if sourceErr != nil {
		findings = append(findings, adapter.Finding{File: ".gitmodules", Entry: relative, Want: locked.Git, Got: sourceErr.Error()})
	} else if source != locked.Git {
		findings = append(findings, adapter.Finding{File: ".gitmodules", Entry: relative, Want: locked.Git, Got: source})
	}
	dest := filepath.Join(root, filepath.FromSlash(relative))
	if _, statErr := os.Stat(filepath.Join(dest, ".git")); statErr != nil {
		findings = append(findings, adapter.Finding{File: relative, Entry: "checkout", Want: locked.Commit, Got: "missing", Repairable: true})
		return findings, nil
	}
	head, headErr := adapter.CommandOutput(ctx, dest, "git", "rev-parse", "HEAD")
	if headErr != nil {
		return nil, headErr
	}
	if got := strings.TrimSpace(string(head)); got != locked.Commit {
		findings = append(findings, adapter.Finding{File: relative, Entry: "checkout", Want: locked.Commit, Got: got, Repairable: true})
	}
	branch, branchErr := adapter.CommandOutput(ctx, dest, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if branchErr != nil {
		return nil, branchErr
	}
	if got := strings.TrimSpace(string(branch)); got != "HEAD" {
		findings = append(findings, adapter.Finding{File: relative, Entry: "HEAD", Want: "detached", Got: got, Repairable: true})
	}
	dirty, dirtyErr := adapter.CommandOutput(ctx, dest, "git", "status", "--porcelain", "--untracked-files=all")
	if dirtyErr != nil {
		return nil, dirtyErr
	}
	if got := strings.TrimSpace(string(dirty)); got != "" {
		findings = append(findings, adapter.Finding{File: relative, Entry: "worktree", Want: "clean", Got: got})
	}
	origin, originErr := adapter.CommandOutput(ctx, dest, "git", "remote", "get-url", "origin")
	if originErr != nil {
		findings = append(findings, adapter.Finding{File: relative, Entry: "origin", Want: locked.Git, Got: "missing"})
	} else if got := strings.TrimSpace(string(origin)); got != locked.Git {
		findings = append(findings, adapter.Finding{File: relative, Entry: "origin", Want: locked.Git, Got: got})
	}
	return findings, nil
}

func (Adapter) apply(ctx context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (change adapter.Change, err error) {
	relative, err := bindingPath(root, dep, exp)
	if err != nil {
		return change, err
	}
	if !objectID.MatchString(locked.Commit) {
		return change, fmt.Errorf("submodule: locked commit must be a 40-character hexadecimal object ID")
	}
	if dep.Git == "" || locked.Git == "" || dep.Git != locked.Git {
		return change, fmt.Errorf("submodule: dependency and lock must name the same non-empty Git source")
	}
	if err = requireRepository(ctx, root); err != nil {
		return change, err
	}
	if err = rejectConflicts(ctx, root, relative); err != nil {
		return change, err
	}
	if err = validateGitmodules(ctx, root); err != nil {
		return change, err
	}
	entry, err := readIndexEntry(ctx, root, relative)
	if err != nil {
		return change, err
	}
	if entry != nil && entry.mode != "160000" {
		return change, fmt.Errorf("submodule path %q is tracked with mode %s, not as a gitlink", relative, entry.mode)
	}

	dest := filepath.Join(root, filepath.FromSlash(relative))
	created := entry == nil
	gitmodules := filepath.Join(root, ".gitmodules")
	originalModules, readErr := os.ReadFile(gitmodules)
	hadModules := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return change, readErr
	}
	if created {
		if _, statErr := os.Lstat(dest); statErr == nil {
			return change, fmt.Errorf("submodule path %q already exists and is not owned by this binding", relative)
		} else if !os.IsNotExist(statErr) {
			return change, statErr
		}
		if section, source, registerErr := registeredSource(root, relative); registerErr == nil && section != "" {
			return change, fmt.Errorf("submodule path %q is already registered for %q", relative, source)
		}
		defer func() {
			if err != nil {
				rollbackNew(ctx, root, relative, originalModules, hadModules)
			}
		}()
		args := []string{"submodule", "add", "--", dep.Git, filepath.FromSlash(relative)}
		if _, err = gitSourceCommand(ctx, root, dep.Git, args...); err != nil {
			return change, err
		}
	} else {
		section, source, registerErr := registeredSource(root, relative)
		if registerErr != nil {
			return change, registerErr
		}
		if section == "" {
			return change, fmt.Errorf("submodule path %q has no matching .gitmodules registration", relative)
		}
		if source != dep.Git {
			return change, fmt.Errorf("submodule path %q belongs to %q, not %q", relative, source, dep.Git)
		}
		if _, statErr := os.Stat(filepath.Join(dest, ".git")); os.IsNotExist(statErr) {
			if _, err = gitSourceCommand(ctx, root, dep.Git, "submodule", "update", "--init", "--", filepath.FromSlash(relative)); err != nil {
				return change, err
			}
		} else if statErr != nil {
			return change, statErr
		}
		if err = requireCleanCheckout(ctx, dest, relative); err != nil {
			return change, err
		}
	}

	oldHead := ""
	if out, headErr := adapter.CommandOutput(ctx, dest, "git", "rev-parse", "HEAD"); headErr == nil {
		oldHead = strings.TrimSpace(string(out))
	}
	if err = requireOrigin(ctx, dest, relative, dep.Git); err != nil {
		return change, err
	}
	if _, err = gitSourceCommand(ctx, dest, dep.Git, "fetch", "origin", locked.Commit); err != nil {
		return change, err
	}
	if err = adapter.Command(ctx, dest, "git", "checkout", "--detach", locked.Commit); err != nil {
		return change, err
	}
	if err = adapter.Command(ctx, root, "git", "add", "--", ".gitmodules", filepath.FromSlash(relative)); err != nil {
		if !created && oldHead != "" {
			_ = adapter.Command(ctx, dest, "git", "checkout", "--detach", oldHead)
			_ = adapter.Command(ctx, root, "git", "update-index", "--add", "--cacheinfo", "160000,"+entry.oid+","+relative)
		}
		return change, err
	}
	branch, err := adapter.CommandOutput(ctx, dest, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || strings.TrimSpace(string(branch)) != "HEAD" {
		if err == nil {
			err = fmt.Errorf("submodule %q did not enter detached HEAD state", relative)
		}
		return change, err
	}
	changed := created || entry.oid != locked.Commit
	return adapter.Change{File: relative, Entry: dep.Name, Changed: changed}, nil
}

var objectID = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

type indexEntry struct{ mode, oid string }

func bindingPath(root string, dep adapter.Dependency, exp adapter.Export) (string, error) {
	relative := exp.Path
	if relative == "" {
		if dep.Name == "" {
			return "", fmt.Errorf("submodule: dependency alias is required when binding path is empty")
		}
		relative = filepath.Join("deps", dep.Name)
	}
	relative = filepath.ToSlash(filepath.Clean(relative))
	if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, "../") {
		return "", fmt.Errorf("submodule path %q must be relative and ..-free", exp.Path)
	}
	if relative == ".git" || strings.HasPrefix(relative, ".git/") || relative == ".git-a2a" || strings.HasPrefix(relative, ".git-a2a/") {
		return "", fmt.Errorf("submodule path %q must not be inside .git or .git-a2a", relative)
	}
	current := root
	parts := strings.Split(relative, "/")
	for _, part := range parts {
		current = filepath.Join(current, filepath.FromSlash(part))
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("submodule path %q contains symlink component %q", relative, current)
		}
	}
	return relative, nil
}

func requireRepository(ctx context.Context, root string) error {
	if err := adapter.RequireTool(ctx, "git", ""); err != nil {
		return err
	}
	top, err := adapter.CommandOutput(ctx, root, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("submodule: consumer root is not a Git repository: %w", err)
	}
	want, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if evaluated, evalErr := filepath.EvalSymlinks(want); evalErr == nil {
		want = evaluated
	}
	got, err := filepath.Abs(strings.TrimSpace(string(top)))
	if err != nil {
		return err
	}
	if evaluated, evalErr := filepath.EvalSymlinks(got); evalErr == nil {
		got = evaluated
	}
	if filepath.Clean(got) != filepath.Clean(want) {
		return fmt.Errorf("submodule: consumer root %q is inside repository %q; pass the repository root", root, got)
	}
	return nil
}

func rejectConflicts(ctx context.Context, root, relative string) error {
	out, err := adapter.CommandOutput(ctx, root, "git", "ls-files", "-u", "--", ".gitmodules", filepath.FromSlash(relative))
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(out)) != 0 {
		return fmt.Errorf("submodule %q has unresolved index conflicts", relative)
	}
	return nil
}

func validateGitmodules(ctx context.Context, root string) error {
	path := filepath.Join(root, ".gitmodules")
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf(".gitmodules is a symlink; refusing to edit it")
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf(".gitmodules is not a regular file; refusing to edit it")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	tracked, runErr := adapter.CommandOutput(ctx, root, "git", "ls-files", "-s", "--", ".gitmodules")
	if runErr != nil {
		return runErr
	}
	if len(bytes.TrimSpace(tracked)) != 0 {
		return fmt.Errorf(".gitmodules is locally deleted; restore or commit that change before continuing")
	}
	return nil
}

func readIndexEntry(ctx context.Context, root, relative string) (*indexEntry, error) {
	out, err := adapter.CommandOutput(ctx, root, "git", "ls-files", "-s", "--", filepath.FromSlash(relative))
	if err != nil {
		return nil, err
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return nil, nil
	}
	lines := strings.Split(line, "\n")
	if len(lines) != 1 {
		return nil, fmt.Errorf("submodule path %q has ambiguous index entries", relative)
	}
	fields := strings.Fields(lines[0])
	if len(fields) < 3 || fields[2] != "0" {
		return nil, fmt.Errorf("submodule path %q has an unresolved index entry", relative)
	}
	return &indexEntry{mode: fields[0], oid: fields[1]}, nil
}

func registeredSource(root, relative string) (section, source string, err error) {
	body, readErr := os.ReadFile(filepath.Join(root, ".gitmodules"))
	if os.IsNotExist(readErr) {
		return "", "", nil
	}
	if readErr != nil {
		return "", "", readErr
	}
	sections := parseGitmodules(body)
	for name, values := range sections {
		if filepath.ToSlash(filepath.Clean(values["path"])) == relative {
			return name, values["url"], nil
		}
	}
	return "", "", nil
}

func parseGitmodules(body []byte) map[string]map[string]string {
	result := map[string]map[string]string{}
	section := ""
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "[submodule \"") && strings.HasSuffix(line, "\"]") {
			section = strings.TrimSuffix(strings.TrimPrefix(line, "[submodule \""), "\"]")
			if result[section] == nil {
				result[section] = map[string]string{}
			}
			continue
		}
		if section == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok {
			result[section][strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return result
}

func requireCleanCheckout(ctx context.Context, dest, relative string) error {
	dirty, err := adapter.CommandOutput(ctx, dest, "git", "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("inspect submodule %q: %w", relative, err)
	}
	if got := strings.TrimSpace(string(dirty)); got != "" {
		return fmt.Errorf("submodule %q is dirty; preserve or discard its changes before continuing: %s", relative, got)
	}
	return nil
}

func requireOrigin(ctx context.Context, dest, relative, want string) error {
	out, err := adapter.CommandOutput(ctx, dest, "git", "remote", "get-url", "origin")
	if err != nil {
		return fmt.Errorf("inspect submodule %q origin: %w", relative, err)
	}
	if got := strings.TrimSpace(string(out)); got != want {
		return fmt.Errorf("submodule path %q origin is %q, not %q", relative, got, want)
	}
	return nil
}

func gitSourceCommand(ctx context.Context, root, source string, args ...string) ([]byte, error) {
	if strings.HasPrefix(source, "file://") {
		args = append([]string{"-c", "protocol.file.allow=always"}, args...)
	}
	return adapter.CommandOutput(ctx, root, "git", args...)
}

func removeSection(path, name string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(body), "\n")
	header := `[submodule "` + name + `"]`
	start, end := -1, len(lines)
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == header {
			start = i
			continue
		}
		if start >= 0 && strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			end = i
			break
		}
	}
	if start < 0 {
		return fmt.Errorf("submodule registration %q disappeared from .gitmodules", name)
	}
	next := append(append([]string{}, lines[:start]...), lines[end:]...)
	return os.WriteFile(path, []byte(strings.Join(next, "")), 0o644)
}

func rollbackNew(ctx context.Context, root, relative string, modules []byte, hadModules bool) {
	_ = os.RemoveAll(filepath.Join(root, filepath.FromSlash(relative)))
	_ = adapter.Command(ctx, root, "git", "update-index", "--remove", "--", filepath.FromSlash(relative))
	restoreFile(filepath.Join(root, ".gitmodules"), modules, hadModules)
	_ = adapter.Command(ctx, root, "git", "add", "-A", "--", ".gitmodules")
	removeModuleStore(ctx, root, relative)
}

func restoreFile(path string, body []byte, existed bool) {
	if existed {
		_ = os.WriteFile(path, body, 0o644)
	} else {
		_ = os.Remove(path)
	}
}

func removeLocalConfigSection(ctx context.Context, root, section string) {
	_ = adapter.Command(ctx, root, "git", "config", "--remove-section", "submodule."+section)
}

func removeModuleStore(ctx context.Context, root, relative string) {
	out, err := adapter.CommandOutput(ctx, root, "git", "rev-parse", "--git-common-dir")
	if err != nil {
		return
	}
	common := strings.TrimSpace(string(out))
	if !filepath.IsAbs(common) {
		common = filepath.Join(root, common)
	}
	modulesRoot := filepath.Join(common, "modules")
	store := filepath.Join(modulesRoot, filepath.FromSlash(relative))
	_ = os.RemoveAll(store)
	for current := filepath.Dir(store); pathWithin(current, modulesRoot); current = filepath.Dir(current) {
		entries, readErr := os.ReadDir(current)
		if readErr != nil || len(entries) != 0 {
			break
		}
		_ = os.Remove(current)
		if filepath.Clean(current) == filepath.Clean(modulesRoot) {
			break
		}
	}
}

func pathWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

var _ adapter.Adapter = Adapter{}
