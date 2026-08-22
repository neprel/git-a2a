package fetch

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/neprel/git-a2a/internal/gitx"
)

type Result struct {
	Manifest            []byte
	Commit, Ref, Method string
}

type Fetcher struct{ Runner gitx.Runner }

func IsMissingManifest(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "a2amodule.yml") && (strings.Contains(message, "does not exist") || strings.Contains(message, "not present") || strings.Contains(message, "not found"))
}

func (f Fetcher) Fetch(ctx context.Context, url, ref, modulePath, work string) (Result, error) {
	if f.Runner == nil {
		return Result{}, fmt.Errorf("fetch: runner is required")
	}
	if modulePath == "" {
		modulePath = "."
	}
	resolution, err := gitx.ResolveDetailed(ctx, f.Runner, url, ref)
	if err != nil {
		return Result{}, err
	}
	commit := resolution.Commit
	manifestPath := path.Join(modulePath, "a2amodule.yml")
	if b, err := f.archiveResolved(ctx, url, resolution, manifestPath); err == nil {
		return Result{Manifest: b, Commit: commit, Ref: resolution.FullRef, Method: "archive"}, nil
	}
	if b, err := f.sparse(ctx, url, commit, manifestPath, work); err == nil {
		return Result{Manifest: b, Commit: commit, Ref: resolution.FullRef, Method: "sparse"}, nil
	}
	b, err := f.shallow(ctx, url, commit, manifestPath, work)
	if err != nil {
		return Result{}, fmt.Errorf("fetch manifest: archive, sparse, and shallow strategies failed: %w", err)
	}
	return Result{Manifest: b, Commit: commit, Ref: resolution.FullRef, Method: "shallow"}, nil
}

func (f Fetcher) archive(ctx context.Context, url, commit, manifestPath string) ([]byte, error) {
	out, err := f.Runner.Run(ctx, "", nil, "archive", "--format=tar", "--remote="+url, commit, manifestPath)
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(bytes.NewReader(out))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if path.Clean(h.Name) == path.Clean(manifestPath) {
			return io.ReadAll(io.LimitReader(tr, 2<<20))
		}
	}
	return nil, fmt.Errorf("%s not present in archive", manifestPath)
}

func (f Fetcher) archiveResolved(ctx context.Context, url string, resolution gitx.Resolution, filePath string) ([]byte, error) {
	body, err := f.archive(ctx, url, resolution.FullRef, filePath)
	if err != nil {
		return nil, err
	}
	confirmed, err := gitx.ResolveDetailed(ctx, f.Runner, url, resolution.FullRef)
	if err != nil || confirmed.Commit != resolution.Commit {
		return nil, fmt.Errorf("remote ref moved while archive was read")
	}
	return body, nil
}

func (f Fetcher) File(ctx context.Context, url, commit, filePath string) ([]byte, error) {
	result, err := f.fileAtCommit(ctx, url, commit, filePath, "")
	return result.Manifest, err
}

// FetchFile resolves ref and returns a single file together with the strategy
// used. It is primarily useful for conformance checks of real git hosts.
func (f Fetcher) FetchFile(ctx context.Context, url, ref, filePath, work string) (Result, error) {
	resolution, err := gitx.ResolveDetailed(ctx, f.Runner, url, ref)
	if err != nil {
		return Result{}, err
	}
	if body, archiveErr := f.archiveResolved(ctx, url, resolution, path.Clean(filePath)); archiveErr == nil {
		return Result{Manifest: body, Commit: resolution.Commit, Ref: resolution.FullRef, Method: "archive"}, nil
	}
	result, err := f.fileAtCommit(ctx, url, resolution.Commit, filePath, work)
	if err != nil {
		return Result{}, err
	}
	result.Ref = resolution.FullRef
	return result, nil
}

func (f Fetcher) fileAtCommit(ctx context.Context, url, commit, filePath, work string) (Result, error) {
	filePath = path.Clean(filePath)
	if b, err := f.archive(ctx, url, commit, filePath); err == nil {
		return Result{Manifest: b, Commit: commit, Method: "archive"}, nil
	}
	cleanup := false
	if work == "" {
		var err error
		work, err = os.MkdirTemp("", "git-a2a-file-")
		if err != nil {
			return Result{}, err
		}
		cleanup = true
	}
	if cleanup {
		defer os.RemoveAll(work)
	}
	if b, err := f.sparse(ctx, url, commit, filePath, work); err == nil {
		return Result{Manifest: b, Commit: commit, Method: "sparse"}, nil
	}
	b, err := f.shallow(ctx, url, commit, filePath, work)
	if err != nil {
		return Result{}, fmt.Errorf("fetch file: archive, sparse, and shallow strategies failed: %w", err)
	}
	return Result{Manifest: b, Commit: commit, Method: "shallow"}, nil
}

func (f Fetcher) sparse(ctx context.Context, url, commit, manifestPath, work string) ([]byte, error) {
	src := filepath.Join(work, ".src")
	if _, err := os.Stat(filepath.Join(src, ".git")); os.IsNotExist(err) {
		if err := os.MkdirAll(work, 0o755); err != nil {
			return nil, err
		}
		if _, err := f.Runner.Run(ctx, "", nil, "clone", "--depth=1", "--filter=blob:none", "--sparse", "--no-checkout", url, src); err != nil {
			return nil, err
		}
	}
	if _, err := f.Runner.Run(ctx, src, nil, "sparse-checkout", "set", "--no-cone", "/"+manifestPath); err != nil {
		return nil, err
	}
	if _, err := f.Runner.Run(ctx, src, nil, "fetch", "--depth=1", "origin", commit); err != nil {
		return nil, err
	}
	if _, err := f.Runner.Run(ctx, src, nil, "checkout", "--detach", commit); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(src, filepath.FromSlash(manifestPath)))
}

func (f Fetcher) shallow(ctx context.Context, url, commit, manifestPath, work string) ([]byte, error) {
	src := filepath.Join(work, ".shallow")
	if err := os.RemoveAll(src); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return nil, err
	}
	if _, err := f.Runner.Run(ctx, "", nil, "clone", "--depth=1", "--no-checkout", url, src); err != nil {
		return nil, err
	}
	if _, err := f.Runner.Run(ctx, src, nil, "fetch", "--depth=1", "origin", commit); err != nil {
		return nil, err
	}
	out, err := f.Runner.Run(ctx, src, nil, "show", commit+":"+manifestPath)
	if err != nil {
		return nil, err
	}
	return out, nil
}

type SurfaceResult struct {
	Files  []string
	Tree   string
	Method string
}

func (f Fetcher) Surface(ctx context.Context, url, commit, modulePath, surface, dest, work string) (SurfaceResult, error) {
	prefix := path.Join(modulePath, surface)
	out, err := f.Runner.Run(ctx, "", nil, "archive", "--format=tar", "--remote="+url, commit, prefix)
	if err == nil {
		files, tree, extractErr := extractSurfaceArchive(out, prefix, dest)
		if extractErr == nil {
			return SurfaceResult{Files: files, Tree: tree, Method: "archive"}, nil
		}
	}
	if result, sparseErr := f.surfaceCheckout(ctx, url, commit, prefix, dest, filepath.Join(work, ".surface-src"), true); sparseErr == nil {
		result.Method = "sparse"
		return result, nil
	}
	result, shallowErr := f.surfaceCheckout(ctx, url, commit, prefix, dest, filepath.Join(work, ".surface-shallow"), false)
	if shallowErr != nil {
		return SurfaceResult{}, fmt.Errorf("fetch surface: archive, sparse, and shallow strategies failed: %w", shallowErr)
	}
	result.Method = "shallow"
	return result, nil
}

func extractSurfaceArchive(out []byte, prefix, dest string) ([]string, string, error) {
	if err := os.RemoveAll(dest); err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, "", err
	}
	tr := tar.NewReader(bytes.NewReader(out))
	var names []string
	tree := newSurfaceTree()
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(path.Clean(h.Name), path.Clean(prefix)), "/")
		if rel == "" || strings.HasPrefix(rel, "../") {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if h.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return nil, "", err
			}
			continue
		}
		if !h.FileInfo().Mode().IsRegular() {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, "", err
		}
		b, err := io.ReadAll(io.LimitReader(tr, 8<<20))
		if err != nil {
			return nil, "", err
		}
		if err := os.WriteFile(target, b, 0o644); err != nil {
			return nil, "", err
		}
		mode := "100644"
		if h.FileInfo().Mode()&0o111 != 0 {
			mode = "100755"
		}
		tree.add(rel, mode, gitObjectID("blob", b))
		names = append(names, rel)
	}
	if len(names) == 0 {
		return nil, "", fmt.Errorf("surface %s is empty", prefix)
	}
	return names, "tree:" + tree.id(), nil
}

func (f Fetcher) surfaceCheckout(ctx context.Context, url, commit, prefix, dest, src string, sparse bool) (SurfaceResult, error) {
	if err := os.RemoveAll(src); err != nil {
		return SurfaceResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		return SurfaceResult{}, err
	}
	cloneArgs := []string{"clone", "--depth=1", "--no-checkout"}
	if sparse {
		cloneArgs = append(cloneArgs, "--filter=blob:none", "--sparse")
	}
	cloneArgs = append(cloneArgs, url, src)
	if _, err := f.Runner.Run(ctx, "", nil, cloneArgs...); err != nil {
		return SurfaceResult{}, err
	}
	if sparse {
		if _, err := f.Runner.Run(ctx, src, nil, "sparse-checkout", "set", "--no-cone", "/"+prefix+"/"); err != nil {
			return SurfaceResult{}, err
		}
	}
	if _, err := f.Runner.Run(ctx, src, nil, "fetch", "--depth=1", "origin", commit); err != nil {
		return SurfaceResult{}, err
	}
	if _, err := f.Runner.Run(ctx, src, nil, "checkout", "--detach", commit); err != nil {
		return SurfaceResult{}, err
	}
	treeRaw, err := f.Runner.Run(ctx, src, nil, "rev-parse", commit+":"+prefix)
	if err != nil {
		return SurfaceResult{}, err
	}
	files, err := copySurface(filepath.Join(src, filepath.FromSlash(prefix)), dest)
	if err != nil {
		return SurfaceResult{}, err
	}
	return SurfaceResult{Files: files, Tree: "tree:" + strings.TrimSpace(string(treeRaw))}, nil
}

func copySurface(source, dest string) ([]string, error) {
	if err := os.RemoveAll(dest); err != nil {
		return nil, err
	}
	var names []string
	err := filepath.WalkDir(source, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, filePath)
		if err != nil || rel == "." {
			return err
		}
		target := filepath.Join(dest, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		body, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, body, info.Mode().Perm()); err != nil {
			return err
		}
		names = append(names, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(names)
	return names, err
}

type surfaceTree struct {
	files map[string]surfaceEntry
	dirs  map[string]*surfaceTree
}

type surfaceEntry struct {
	mode string
	oid  []byte
}

func newSurfaceTree() *surfaceTree {
	return &surfaceTree{files: map[string]surfaceEntry{}, dirs: map[string]*surfaceTree{}}
}

func (t *surfaceTree) add(name, mode string, oid []byte) {
	parts := strings.Split(path.Clean(name), "/")
	current := t
	for _, part := range parts[:len(parts)-1] {
		if current.dirs[part] == nil {
			current.dirs[part] = newSurfaceTree()
		}
		current = current.dirs[part]
	}
	current.files[parts[len(parts)-1]] = surfaceEntry{mode: mode, oid: oid}
}

func (t *surfaceTree) id() string { return hex.EncodeToString(gitObjectID("tree", t.body())) }

func (t *surfaceTree) body() []byte {
	type item struct {
		name, mode string
		oid        []byte
		dir        bool
	}
	items := make([]item, 0, len(t.files)+len(t.dirs))
	for name, file := range t.files {
		items = append(items, item{name: name, mode: file.mode, oid: file.oid})
	}
	for name, dir := range t.dirs {
		items = append(items, item{name: name, mode: "40000", oid: gitObjectID("tree", dir.body()), dir: true})
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i].name, items[j].name
		if items[i].dir {
			left += "/"
		}
		if items[j].dir {
			right += "/"
		}
		return left < right
	})
	var out []byte
	for _, entry := range items {
		out = append(out, entry.mode...)
		out = append(out, ' ')
		out = append(out, entry.name...)
		out = append(out, 0)
		out = append(out, entry.oid...)
	}
	return out
}

func gitObjectID(kind string, body []byte) []byte {
	h := sha1.New()
	fmt.Fprintf(h, "%s %d%c", kind, len(body), byte(0))
	_, _ = h.Write(body)
	return h.Sum(nil)
}
