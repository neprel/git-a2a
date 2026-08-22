package fetch

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/neprel/git-a2a/internal/gitx"
)

type Result struct {
	Manifest       []byte
	Commit, Method string
}

type Fetcher struct{ Runner gitx.Runner }

func (f Fetcher) Fetch(ctx context.Context, url, ref, modulePath, work string) (Result, error) {
	if f.Runner == nil {
		return Result{}, fmt.Errorf("fetch: runner is required")
	}
	if modulePath == "" {
		modulePath = "."
	}
	commit, err := gitx.Resolve(ctx, f.Runner, url, ref)
	if err != nil {
		return Result{}, err
	}
	manifestPath := path.Join(modulePath, "a2amodule.yml")
	if b, err := f.archive(ctx, url, commit, manifestPath); err == nil {
		return Result{Manifest: b, Commit: commit, Method: "archive"}, nil
	}
	if b, err := f.sparse(ctx, url, commit, manifestPath, work); err == nil {
		return Result{Manifest: b, Commit: commit, Method: "sparse"}, nil
	}
	b, err := f.shallow(ctx, url, commit, manifestPath, work)
	if err != nil {
		return Result{}, fmt.Errorf("fetch manifest: archive, sparse, and shallow strategies failed: %w", err)
	}
	return Result{Manifest: b, Commit: commit, Method: "shallow"}, nil
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

func (f Fetcher) Surface(ctx context.Context, url, commit, modulePath, surface, dest string) ([]string, error) {
	prefix := path.Join(modulePath, surface)
	out, err := f.Runner.Run(ctx, "", nil, "archive", "--format=tar", "--remote="+url, commit, prefix)
	if err != nil {
		return nil, err
	}
	if err := os.RemoveAll(dest); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, err
	}
	tr := tar.NewReader(bytes.NewReader(out))
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(path.Clean(h.Name), path.Clean(prefix)), "/")
		if rel == "" || strings.HasPrefix(rel, "../") {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if h.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return nil, err
			}
			continue
		}
		if !h.FileInfo().Mode().IsRegular() {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(tr, 8<<20))
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, b, 0o644); err != nil {
			return nil, err
		}
		names = append(names, rel)
	}
	return names, nil
}
