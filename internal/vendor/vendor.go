package vendor

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/neprel/git-a2a/internal/gitx"
	"github.com/neprel/git-a2a/internal/manifest"
)

const StampName = ".git-a2a-vendored"

type Manager struct {
	Runner gitx.Runner
}

type Finding struct {
	Kind string
	Want string
	Got  string
}

type State struct {
	Mode     string
	Path     string
	Label    string
	Findings []Finding
}

func Mode(dep manifest.Dependency) string {
	if dep.Vendor == nil {
		return ""
	}
	if dep.Vendor.Mode == "" {
		return "submodule"
	}
	return dep.Vendor.Mode
}

func Path(own *manifest.Manifest, dep manifest.Dependency) string {
	if dep.Vendor != nil && dep.Vendor.Path != "" {
		return filepath.ToSlash(filepath.Clean(dep.Vendor.Path))
	}
	dir := "deps"
	if own != nil && own.Settings != nil && own.Settings.VendorDir != "" {
		dir = own.Settings.VendorDir
	}
	return filepath.ToSlash(filepath.Join(dir, dep.ID))
}

func ValidatePath(root, relative string) error {
	if relative == "" {
		return fmt.Errorf("vendor path is empty")
	}
	normal := filepath.ToSlash(filepath.Clean(relative))
	if filepath.IsAbs(relative) || normal == ".." || strings.HasPrefix(normal, "../") {
		return fmt.Errorf("vendor path %q must be relative and ..-free", relative)
	}
	trimmed := strings.TrimPrefix(normal, "./")
	if trimmed == ".git" || strings.HasPrefix(trimmed, ".git/") || trimmed == ".git-a2a" || strings.HasPrefix(trimmed, ".git-a2a/") {
		return fmt.Errorf("vendor path %q must not be inside .git or .git-a2a", relative)
	}
	current := root
	for _, part := range strings.Split(trimmed, "/") {
		current = filepath.Join(current, filepath.FromSlash(part))
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return fmt.Errorf("inspect vendor path %q: %w", relative, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("vendor path %q contains symlink component %q", relative, current)
		}
	}
	return nil
}

func (m Manager) Apply(ctx context.Context, root string, own *manifest.Manifest, dep manifest.Dependency, locked manifest.LockedDependency, force bool) (*manifest.LockedVendor, error) {
	var previous *manifest.LockedDependency
	if locked.Vendor != nil {
		previous = &locked
	}
	return m.ApplyTransition(ctx, root, own, dep, locked, previous, force)
}

func (m Manager) ApplyTransition(ctx context.Context, root string, own *manifest.Manifest, dep manifest.Dependency, locked manifest.LockedDependency, previous *manifest.LockedDependency, force bool) (*manifest.LockedVendor, error) {
	if m.Runner == nil {
		return nil, fmt.Errorf("vendor: runner is required")
	}
	if dep.Vendor == nil {
		return nil, nil
	}
	relative := Path(own, dep)
	if err := ValidatePath(root, relative); err != nil {
		return nil, err
	}
	mode := Mode(dep)
	if previous != nil && previous.Vendor != nil {
		current, inspectErr := m.Inspect(ctx, root, dep, *previous)
		if inspectErr == nil && unsafeFinding(current.Findings) != nil && !force {
			finding := unsafeFinding(current.Findings)
			return nil, fmt.Errorf("vendored dependency %s is dirty or drifted (%s); rerun with --force to replace it", dep.ID, finding.Kind)
		}
		if inspectErr != nil && !errors.Is(inspectErr, os.ErrNotExist) {
			return nil, inspectErr
		}
	}
	switch mode {
	case "submodule":
		if err := m.applySubmodule(ctx, root, dep, locked, relative); err != nil {
			return nil, err
		}
		return &manifest.LockedVendor{Mode: mode, Path: relative}, nil
	case "copy":
		tree, err := m.applyCopy(ctx, root, dep, locked, relative)
		if err != nil {
			return nil, err
		}
		return &manifest.LockedVendor{Mode: mode, Path: relative, Tree: "tree:" + tree}, nil
	default:
		return nil, fmt.Errorf("vendor mode %q is not supported", mode)
	}
}

func unsafeFinding(findings []Finding) *Finding {
	for i := range findings {
		switch findings[i].Kind {
		case "dirty", "tree", "gitlink", "checkout":
			return &findings[i]
		}
	}
	return nil
}

func (m Manager) Inspect(ctx context.Context, root string, dep manifest.Dependency, locked manifest.LockedDependency) (State, error) {
	state := State{Mode: Mode(dep)}
	if locked.Vendor != nil {
		state.Mode = locked.Vendor.Mode
		state.Path = locked.Vendor.Path
	} else if dep.Vendor != nil {
		state.Path = dep.Vendor.Path
	}
	if state.Path == "" {
		state.Path = filepath.ToSlash(filepath.Join("deps", dep.ID))
	}
	if err := ValidatePath(root, state.Path); err != nil {
		return state, err
	}
	switch state.Mode {
	case "submodule":
		return m.inspectSubmodule(ctx, root, locked, state)
	case "copy":
		return m.inspectCopy(ctx, root, locked, state)
	default:
		return state, fmt.Errorf("vendor mode %q is not supported", state.Mode)
	}
}

func (m Manager) Remove(ctx context.Context, root string, dep manifest.Dependency, locked manifest.LockedDependency, force bool) error {
	if locked.Vendor == nil && dep.Vendor == nil {
		return nil
	}
	state, err := m.Inspect(ctx, root, dep, locked)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && unsafeFinding(state.Findings) != nil && !force {
		finding := unsafeFinding(state.Findings)
		return fmt.Errorf("vendored dependency %s is dirty or drifted (%s); rerun with --force to remove it", dep.ID, finding.Kind)
	}
	path := state.Path
	if locked.Vendor != nil {
		path = locked.Vendor.Path
	}
	if state.Mode == "submodule" {
		return m.removeSubmodule(ctx, root, path)
	}
	if _, runErr := m.run(ctx, root, dep.Git, "rm", "-r", "-f", "--ignore-unmatch", "--", filepath.FromSlash(path)); runErr != nil {
		return runErr
	}
	return os.RemoveAll(filepath.Join(root, filepath.FromSlash(path)))
}

func (m Manager) applySubmodule(ctx context.Context, root string, dep manifest.Dependency, locked manifest.LockedDependency, relative string) (err error) {
	gitmodules := filepath.Join(root, ".gitmodules")
	original, readErr := os.ReadFile(gitmodules)
	hadGitmodules := readErr == nil
	created := false
	defer func() {
		if err == nil || !created {
			return
		}
		_ = m.removeSubmodule(ctx, root, relative)
		if hadGitmodules {
			_ = os.WriteFile(gitmodules, original, 0o644)
			_, _ = m.run(ctx, root, dep.Git, "add", "--", ".gitmodules")
		} else {
			_ = os.Remove(gitmodules)
			_, _ = m.run(ctx, root, dep.Git, "rm", "--cached", "--ignore-unmatch", "--", ".gitmodules")
		}
	}()

	index, _ := m.run(ctx, root, dep.Git, "ls-files", "-s", "--", filepath.FromSlash(relative))
	if len(bytes.TrimSpace(index)) == 0 {
		args := []string{"submodule", "add"}
		if dep.Track == "floating" && dep.Ref != "" && len(dep.Ref) != 40 {
			args = append(args, "-b", dep.Ref)
		}
		args = append(args, "--", dep.Git, filepath.FromSlash(relative))
		if _, err = m.run(ctx, root, dep.Git, args...); err != nil {
			return err
		}
		created = true
	}
	dest := filepath.Join(root, filepath.FromSlash(relative))
	if _, statErr := os.Stat(filepath.Join(dest, ".git")); os.IsNotExist(statErr) {
		if _, err = m.run(ctx, root, dep.Git, "submodule", "update", "--init", "--", filepath.FromSlash(relative)); err != nil {
			return err
		}
	}
	if _, err = m.run(ctx, dest, dep.Git, "fetch", "origin", locked.Commit); err != nil {
		return err
	}
	if _, err = m.run(ctx, dest, dep.Git, "checkout", "--detach", locked.Commit); err != nil {
		return err
	}
	if dep.Vendor != nil && dep.Vendor.Recursive {
		if _, err = m.run(ctx, root, dep.Git, "submodule", "update", "--init", "--recursive", "--", filepath.FromSlash(relative)); err != nil {
			return err
		}
	}
	_, err = m.run(ctx, root, dep.Git, "add", "--", ".gitmodules", filepath.FromSlash(relative))
	return err
}

func (m Manager) inspectSubmodule(ctx context.Context, root string, locked manifest.LockedDependency, state State) (State, error) {
	index, err := m.run(ctx, root, locked.Git, "ls-files", "-s", "--", filepath.FromSlash(state.Path))
	if err != nil {
		return state, err
	}
	fields := strings.Fields(string(index))
	if len(fields) < 2 {
		state.Label = "missing"
		state.Findings = append(state.Findings, Finding{Kind: "missing", Want: locked.Commit, Got: ""})
		return state, nil
	}
	if fields[0] != "160000" || fields[1] != locked.Commit {
		state.Findings = append(state.Findings, Finding{Kind: "gitlink", Want: "160000 " + locked.Commit, Got: fields[0] + " " + fields[1]})
	}
	dest := filepath.Join(root, filepath.FromSlash(state.Path))
	if _, statErr := os.Stat(filepath.Join(dest, ".git")); statErr != nil {
		state.Label = "missing"
		state.Findings = append(state.Findings, Finding{Kind: "uninitialised", Want: locked.Commit, Got: "missing"})
		return state, nil
	}
	head, headErr := m.run(ctx, dest, locked.Git, "rev-parse", "HEAD")
	if headErr != nil {
		return state, headErr
	}
	if got := strings.TrimSpace(string(head)); got != locked.Commit {
		state.Findings = append(state.Findings, Finding{Kind: "checkout", Want: locked.Commit, Got: got})
	}
	dirty, dirtyErr := m.run(ctx, dest, locked.Git, "status", "--porcelain", "--untracked-files=all")
	if dirtyErr != nil {
		return state, dirtyErr
	}
	if got := strings.TrimSpace(string(dirty)); got != "" {
		state.Findings = append(state.Findings, Finding{Kind: "dirty", Want: "clean", Got: got})
	}
	state.Label = "submodule @" + short(locked.Commit)
	if len(state.Findings) > 0 {
		state.Label = "drift"
	}
	return state, nil
}

func (m Manager) removeSubmodule(ctx context.Context, root, relative string) error {
	if relative == "" {
		return nil
	}
	_, _ = m.run(ctx, root, "", "submodule", "deinit", "-f", "--", filepath.FromSlash(relative))
	_, _ = m.run(ctx, root, "", "rm", "-f", "--ignore-unmatch", "--", filepath.FromSlash(relative))
	_ = os.RemoveAll(filepath.Join(root, filepath.FromSlash(relative)))
	common, err := m.run(ctx, root, "", "rev-parse", "--git-common-dir")
	if err == nil {
		commonDir := strings.TrimSpace(string(common))
		if !filepath.IsAbs(commonDir) {
			commonDir = filepath.Join(root, commonDir)
		}
		_ = os.RemoveAll(filepath.Join(commonDir, "modules", filepath.FromSlash(relative)))
	}
	gitmodules := filepath.Join(root, ".gitmodules")
	if body, readErr := os.ReadFile(gitmodules); readErr == nil && len(bytes.TrimSpace(body)) == 0 {
		_ = os.Remove(gitmodules)
		_, _ = m.run(ctx, root, "", "rm", "--cached", "--ignore-unmatch", "--", ".gitmodules")
	}
	return nil
}

func (m Manager) applyCopy(ctx context.Context, root string, dep manifest.Dependency, locked manifest.LockedDependency, relative string) (string, error) {
	parent := filepath.Dir(filepath.Join(root, filepath.FromSlash(relative)))
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	temp, err := os.MkdirTemp(parent, ".git-a2a-vendor-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temp)
	src := filepath.Join(temp, "source")
	if _, err = m.run(ctx, "", dep.Git, "clone", "--no-checkout", "--", dep.Git, src); err != nil {
		return "", err
	}
	if _, err = m.run(ctx, src, dep.Git, "fetch", "origin", locked.Commit); err != nil {
		return "", err
	}
	treeish := locked.Commit + "^{tree}"
	if locked.Path != "" && locked.Path != "." {
		treeish = locked.Commit + ":" + filepath.ToSlash(locked.Path)
	}
	treeRaw, err := m.run(ctx, src, dep.Git, "rev-parse", treeish)
	if err != nil {
		return "", err
	}
	tree := strings.TrimSpace(string(treeRaw))
	entries, err := m.run(ctx, src, dep.Git, "ls-tree", "-r", "-z", treeish)
	if err != nil {
		return "", err
	}
	for _, entry := range bytes.Split(entries, []byte{0}) {
		if strings.HasPrefix(string(entry), "160000 ") {
			return "", fmt.Errorf("copy mode cannot materialise nested submodule entry %q", entry)
		}
	}
	archive, err := m.run(ctx, src, dep.Git, "archive", "--format=tar", treeish)
	if err != nil {
		return "", err
	}
	staged := filepath.Join(temp, "tree")
	if err = extractArchive(archive, staged); err != nil {
		return "", err
	}
	stamp := []byte(fmt.Sprintf("id: %s\ncommit: %s\ntree: tree:%s\n", dep.ID, locked.Commit, tree))
	if err = os.WriteFile(filepath.Join(staged, StampName), stamp, 0o644); err != nil {
		return "", err
	}
	dest := filepath.Join(root, filepath.FromSlash(relative))
	backup := filepath.Join(temp, "previous")
	hadDest := false
	if _, statErr := os.Lstat(dest); statErr == nil {
		hadDest = true
		if err = os.Rename(dest, backup); err != nil {
			return "", err
		}
	}
	rollback := func() {
		_ = os.RemoveAll(dest)
		if hadDest {
			_ = os.Rename(backup, dest)
		}
	}
	if err = os.Rename(staged, dest); err != nil {
		rollback()
		return "", err
	}
	if _, err = m.run(ctx, root, dep.Git, "add", "-A", "--", filepath.FromSlash(relative)); err != nil {
		rollback()
		return "", err
	}
	return tree, nil
}

func (m Manager) inspectCopy(ctx context.Context, root string, locked manifest.LockedDependency, state State) (State, error) {
	dest := filepath.Join(root, filepath.FromSlash(state.Path))
	if _, err := os.Stat(dest); err != nil {
		if os.IsNotExist(err) {
			state.Label = "missing"
			state.Findings = append(state.Findings, Finding{Kind: "missing", Want: locked.Vendor.Tree, Got: ""})
			return state, nil
		}
		return state, err
	}
	actual, err := m.indexTree(ctx, root, state.Path)
	if err != nil {
		return state, err
	}
	want := strings.TrimPrefix(locked.Vendor.Tree, "tree:")
	if actual != want {
		state.Findings = append(state.Findings, Finding{Kind: "tree", Want: want, Got: actual})
	}
	state.Label = "copy"
	if len(state.Findings) > 0 {
		state.Label = "drift"
	}
	return state, nil
}

func (m Manager) indexTree(ctx context.Context, root, relative string) (string, error) {
	out, err := m.run(ctx, root, "", "ls-files", "-s", "-z", "--", filepath.FromSlash(relative))
	if err != nil {
		return "", err
	}
	prefix := strings.TrimSuffix(filepath.ToSlash(filepath.Clean(relative)), "/") + "/"
	rootTree := newTree()
	for _, record := range bytes.Split(out, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		meta, nameRaw, ok := bytes.Cut(record, []byte{'\t'})
		if !ok {
			return "", fmt.Errorf("unexpected git index record %q", record)
		}
		fields := strings.Fields(string(meta))
		if len(fields) != 3 || fields[2] != "0" {
			return "", fmt.Errorf("vendor index contains an unresolved stage: %q", record)
		}
		name := strings.TrimPrefix(filepath.ToSlash(string(nameRaw)), prefix)
		if name == StampName {
			continue
		}
		oid, decodeErr := hex.DecodeString(fields[1])
		if decodeErr != nil {
			return "", decodeErr
		}
		rootTree.add(name, fields[0], oid)
	}
	return rootTree.id(), nil
}

func (m Manager) run(ctx context.Context, dir, url string, args ...string) ([]byte, error) {
	if strings.HasPrefix(url, "file://") {
		args = append([]string{"-c", "protocol.file.allow=always"}, args...)
	}
	return m.Runner.Run(ctx, dir, nil, args...)
}

func extractArchive(body []byte, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	reader := tar.NewReader(bytes.NewReader(body))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(filepath.FromSlash(header.Name))
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(filepath.ToSlash(name), "../") {
			return fmt.Errorf("archive entry escapes destination: %q", header.Name)
		}
		target := filepath.Join(dest, name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err = os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if header.Mode&0o111 != 0 {
				mode = 0o755
			}
			data, readErr := io.ReadAll(reader)
			if readErr != nil {
				return readErr
			}
			if err = os.WriteFile(target, data, mode); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err = os.Symlink(header.Linkname, target); err != nil {
				return fmt.Errorf("create vendored symlink %s: %w", header.Name, err)
			}
		}
	}
}

type tree struct {
	files map[string]treeEntry
	dirs  map[string]*tree
}

type treeEntry struct {
	mode string
	oid  []byte
}

func newTree() *tree { return &tree{files: map[string]treeEntry{}, dirs: map[string]*tree{}} }

func (t *tree) add(name, mode string, oid []byte) {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(name)), "/")
	current := t
	for _, part := range parts[:len(parts)-1] {
		if current.dirs[part] == nil {
			current.dirs[part] = newTree()
		}
		current = current.dirs[part]
	}
	current.files[parts[len(parts)-1]] = treeEntry{mode: mode, oid: oid}
}

func (t *tree) id() string {
	body := t.body()
	hash := sha1.New()
	fmt.Fprintf(hash, "tree %d%c", len(body), byte(0))
	_, _ = hash.Write(body)
	return hex.EncodeToString(hash.Sum(nil))
}

func (t *tree) body() []byte {
	var names []string
	for name := range t.files {
		names = append(names, name)
	}
	for name := range t.dirs {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		left, right := names[i], names[j]
		if t.dirs[left] != nil {
			left += "/"
		}
		if t.dirs[right] != nil {
			right += "/"
		}
		return left < right
	})
	var out bytes.Buffer
	for _, name := range names {
		mode := "40000"
		var oid []byte
		if file, ok := t.files[name]; ok {
			mode, oid = file.mode, file.oid
		} else {
			decoded, _ := hex.DecodeString(t.dirs[name].id())
			oid = decoded
		}
		fmt.Fprintf(&out, "%s %s%c", mode, name, byte(0))
		_, _ = out.Write(oid)
	}
	return out.Bytes()
}

func short(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}
