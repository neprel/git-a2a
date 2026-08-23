package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/neprel/git-a2a/internal/cache"
	"github.com/neprel/git-a2a/internal/fetch"
	lockfile "github.com/neprel/git-a2a/internal/lock"
	"github.com/neprel/git-a2a/internal/manifest"
	vendortransport "github.com/neprel/git-a2a/internal/vendor"
)

type fetchResult struct {
	ID       string `json:"id"`
	Commit   string `json:"commit"`
	Manifest string `json:"manifest"`
	Surface  string `json:"surface,omitempty"`
	Method   string `json:"method"`
	Vendor   string `json:"vendor,omitempty"`
}

// fetch restores disposable local cache state from the immutable coordinates and hashes in
// a2amodule.lock. It deliberately does not resolve refs, snapshot cards, or write durable files.
func (a *App) fetch(args []string) int {
	wantSurface, jsonOut := false, false
	var ids []string
	for _, arg := range args {
		switch arg {
		case "--surface":
			wantSurface = true
		case "--json":
			jsonOut = true
		default:
			if strings.HasPrefix(arg, "-") {
				fmt.Fprintf(a.Err, "fetch: unknown option %s\n", arg)
				return 2
			}
			ids = append(ids, arg)
		}
	}
	root := a.root()
	if _, err := os.Stat(filepath.Join(root, "a2amodule.lock")); err != nil {
		fmt.Fprintln(a.Err, "fetch: a2amodule.lock is required; run git-a2a add or update first")
		return 1
	}
	own, err := manifest.Load(filepath.Join(root, "a2amodule.yml"))
	if err != nil {
		fmt.Fprintf(a.Err, "fetch: own manifest: %v\n", err)
		return 2
	}
	locked, err := lockfile.Load(root)
	if err != nil {
		fmt.Fprintf(a.Err, "fetch: lock: %v\n", err)
		return 1
	}
	dependencies := make(map[string]manifest.Dependency, len(own.Dependencies))
	for _, dependency := range own.Dependencies {
		dependencies[dependency.ID] = dependency
	}
	if len(ids) == 0 {
		for _, dependency := range own.Dependencies {
			ids = append(ids, dependency.ID)
		}
	} else {
		seen := map[string]bool{}
		unique := ids[:0]
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				unique = append(unique, id)
			}
		}
		ids = unique
	}
	if len(ids) == 0 {
		fmt.Fprintln(a.Err, "fetch: no dependencies")
		return 2
	}

	results := make([]fetchResult, 0, len(ids))
	fetcher := fetch.Fetcher{Runner: a.runner()}
	for _, id := range ids {
		dependency, exists := dependencies[id]
		entry, isLocked := locked.Dependencies[id]
		if !exists || !isLocked || entry.Commit == "" || entry.Manifest == "" {
			fmt.Fprintf(a.Err, "fetch: dependency %s has no complete lock entry\n", id)
			return 1
		}
		work, tempErr := os.MkdirTemp("", "git-a2a-fetch-")
		if tempErr != nil {
			fmt.Fprintf(a.Err, "fetch %s: %v\n", id, tempErr)
			return 1
		}
		res, fetchErr := fetcher.Fetch(a.context(), entry.Git, entry.Commit, defaultPath(entry.Path), filepath.Join(work, "source"))
		if fetchErr != nil {
			_ = os.RemoveAll(work)
			fmt.Fprintf(a.Err, "fetch %s: %v\n", id, fetchErr)
			return 1
		}
		sum := sha256.Sum256(res.Manifest)
		manifestHash := "sha256:" + hex.EncodeToString(sum[:])
		if res.Commit != entry.Commit || manifestHash != entry.Manifest {
			_ = os.RemoveAll(work)
			fmt.Fprintf(a.Err, "fetch %s: locked content mismatch (commit %s, manifest %s)\n", id, entry.Commit, entry.Manifest)
			return 1
		}
		depManifest, parseErr := manifest.Parse(res.Manifest)
		if parseErr != nil {
			_ = os.RemoveAll(work)
			fmt.Fprintf(a.Err, "fetch %s: locked manifest: %v\n", id, parseErr)
			return 1
		}
		stagedRoot := filepath.Join(work, "staged")
		if saveErr := cache.Save(stagedRoot, id, res.Manifest, res.Commit, "lock-"+res.Method); saveErr != nil {
			_ = os.RemoveAll(work)
			fmt.Fprintf(a.Err, "fetch %s: cache: %v\n", id, saveErr)
			return 1
		}
		result := fetchResult{ID: id, Commit: entry.Commit, Manifest: manifestHash, Method: res.Method}
		if wantSurface && depManifest.Module.Surface != "" {
			if entry.Surface == "" {
				_ = os.RemoveAll(work)
				fmt.Fprintf(a.Err, "fetch %s: surface has no hash in a2amodule.lock; run git-a2a show %s --surface first\n", id, id)
				return 1
			}
			surface, surfaceErr := fetcher.Surface(a.context(), dependency.Git, entry.Commit, defaultPath(entry.Path), depManifest.Module.Surface, filepath.Join(cache.Dir(stagedRoot, id), "surface"), filepath.Join(work, "surface-source"))
			if surfaceErr != nil {
				_ = os.RemoveAll(work)
				fmt.Fprintf(a.Err, "fetch %s surface: %v\n", id, surfaceErr)
				return 1
			}
			if surface.Tree != entry.Surface {
				_ = os.RemoveAll(work)
				fmt.Fprintf(a.Err, "fetch %s: locked surface mismatch (want %s, got %s)\n", id, entry.Surface, surface.Tree)
				return 1
			}
			result.Surface = surface.Tree
		}
		if replaceErr := replaceCache(root, id, cache.Dir(stagedRoot, id), work); replaceErr != nil {
			_ = os.RemoveAll(work)
			fmt.Fprintf(a.Err, "fetch %s: cache replacement: %v\n", id, replaceErr)
			return 1
		}
		if dependency.Vendor != nil {
			vendorLock, vendorErr := (vendortransport.Manager{Runner: a.runner()}).Apply(a.context(), root, own, dependency, entry, false)
			if vendorErr != nil {
				_ = os.RemoveAll(work)
				fmt.Fprintf(a.Err, "fetch %s vendor: %v\n", id, vendorErr)
				return 1
			}
			if entry.Vendor == nil || vendorLock.Mode != entry.Vendor.Mode || vendorLock.Path != entry.Vendor.Path || vendorLock.Tree != entry.Vendor.Tree {
				_ = os.RemoveAll(work)
				fmt.Fprintf(a.Err, "fetch %s: vendored content does not match a2amodule.lock\n", id)
				return 1
			}
			result.Vendor = vendorLock.Mode + ":" + vendorLock.Path
		}
		_ = os.RemoveAll(work)
		results = append(results, result)
	}
	if jsonOut {
		sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })
		encoded, _ := json.MarshalIndent(results, "", "  ")
		fmt.Fprintln(a.Out, string(encoded))
	} else {
		for _, result := range results {
			line := fmt.Sprintf("%s: fetched %s", result.ID, result.Commit)
			if result.Surface != "" {
				line += " surface " + result.Surface
			}
			fmt.Fprintln(a.Out, line)
		}
	}
	fmt.Fprintf(a.Err, "fetched %d locked dependency cache(s)\n", len(results))
	return 0
}
