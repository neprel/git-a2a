package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/neprel/git-a2a/adapters"
	"github.com/neprel/git-a2a/internal/adapter"
	"github.com/neprel/git-a2a/internal/cache"
	"github.com/neprel/git-a2a/internal/fetch"
	"github.com/neprel/git-a2a/internal/gitx"
	lockfile "github.com/neprel/git-a2a/internal/lock"
	"github.com/neprel/git-a2a/internal/manifest"
)

type setOptions struct {
	id                           string
	git, ref, path, track, newID *string
	dry                          bool
}

func parseSet(args []string) (setOptions, error) {
	o := setOptions{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		value := func() (*string, error) {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("%s needs a value", arg)
			}
			i++
			v := args[i]
			return &v, nil
		}
		var v *string
		var err error
		switch arg {
		case "--git":
			v, err = value()
			o.git = v
		case "--ref":
			v, err = value()
			o.ref = v
		case "--path":
			v, err = value()
			o.path = v
		case "--track":
			v, err = value()
			o.track = v
		case "--id":
			v, err = value()
			o.newID = v
		case "--dry-run":
			o.dry = true
		default:
			if strings.HasPrefix(arg, "-") {
				return o, fmt.Errorf("unknown option %s", arg)
			}
			if o.id != "" {
				return o, fmt.Errorf("only one dependency id is allowed")
			}
			o.id = arg
		}
		if err != nil {
			return o, err
		}
	}
	if o.id == "" {
		return o, fmt.Errorf("dependency id is required")
	}
	if o.git == nil && o.ref == nil && o.path == nil && o.track == nil && o.newID == nil {
		return o, fmt.Errorf("at least one change option is required")
	}
	if o.track != nil && *o.track != "locked" && *o.track != "floating" {
		return o, fmt.Errorf("--track must be locked or floating")
	}
	return o, nil
}

func (a *App) set(args []string) int {
	o, err := parseSet(args)
	if err != nil {
		fmt.Fprintf(a.Err, "set: %v\n", err)
		return 2
	}
	return a.applySet(o)
}
func (a *App) pin(args []string) int {
	if len(args) < 1 || len(args) > 2 {
		fmt.Fprintln(a.Err, "pin: expected ID [COMMIT]")
		return 2
	}
	commit := ""
	if len(args) == 2 {
		commit = args[1]
		if len(commit) < 40 && isHex(commit) {
			fmt.Fprintln(a.Err, "pin: COMMIT must be a full 40-character SHA; short SHAs are ambiguous")
			return 2
		}
	} else {
		l, err := lockfile.Load(a.root())
		if err != nil {
			fmt.Fprintf(a.Err, "pin: %v\n", err)
			return 1
		}
		entry, ok := l.Dependencies[args[0]]
		if !ok {
			fmt.Fprintf(a.Err, "pin: unknown dependency %s\n", args[0])
			return 2
		}
		commit = entry.Commit
	}
	track := "locked"
	return a.applySet(setOptions{id: args[0], ref: &commit, track: &track})
}
func (a *App) unpin(args []string) int {
	if len(args) < 3 || args[1] != "--ref" {
		fmt.Fprintln(a.Err, "unpin: expected ID --ref BRANCH [--track locked|floating]")
		return 2
	}
	ref := args[2]
	o := setOptions{id: args[0], ref: &ref}
	if len(args) > 3 {
		if len(args) != 5 || args[3] != "--track" {
			fmt.Fprintln(a.Err, "unpin: expected ID --ref BRANCH [--track locked|floating]")
			return 2
		}
		track := args[4]
		if track != "locked" && track != "floating" {
			fmt.Fprintln(a.Err, "unpin: --track must be locked or floating")
			return 2
		}
		o.track = &track
	}
	return a.applySet(o)
}

func (a *App) applySet(o setOptions) int {
	root := a.root()
	own, err := manifest.Load(filepath.Join(root, "a2amodule.yml"))
	if err != nil {
		fmt.Fprintf(a.Err, "set: %v\n", err)
		return 2
	}
	idx := -1
	for i, d := range own.Dependencies {
		if d.ID == o.id {
			idx = i
			break
		}
	}
	if idx < 0 {
		fmt.Fprintf(a.Err, "set: unknown dependency %s\n", o.id)
		return 2
	}
	oldDep := own.Dependencies[idx]
	next := oldDep
	if o.git != nil {
		next.Git = *o.git
	}
	if o.ref != nil {
		next.Ref = *o.ref
	}
	if o.path != nil {
		next.Path = *o.path
	}
	if o.track != nil {
		next.Track = *o.track
	}
	expected := o.id
	if o.newID != nil {
		expected = *o.newID
		next.ID = *o.newID
	}
	if len(next.Ref) == 40 && isHex(next.Ref) {
		next.Track = "locked"
	}
	work, err := os.MkdirTemp("", "git-a2a-set-")
	if err != nil {
		fmt.Fprintf(a.Err, "set: %v\n", err)
		return 1
	}
	defer os.RemoveAll(work)
	f := fetch.Fetcher{Runner: a.runner()}
	l, err := lockfile.Load(root)
	if err != nil {
		fmt.Fprintf(a.Err, "set: lock: %v\n", err)
		return 1
	}
	resolution, resolveErr := gitx.ResolveDetailed(a.context(), a.runner(), next.Git, next.Ref)
	if resolveErr != nil {
		fmt.Fprintf(a.Err, "set: %v\n", resolveErr)
		return 1
	}
	res, err := f.Fetch(a.context(), next.Git, next.Ref, defaultPath(next.Path), filepath.Join(work, "new"))
	if err != nil {
		fmt.Fprintf(a.Err, "set: %v\n", err)
		return 1
	}
	nextManifest, err := manifest.Parse(res.Manifest)
	if err != nil {
		fmt.Fprintf(a.Err, "set: fetched manifest: %v\n", err)
		return 1
	}
	if nextManifest.Module.ID != expected {
		fmt.Fprintf(a.Err, "set: module id mismatch: expected %s, fetched %s; no files changed\n", expected, nextManifest.Module.ID)
		return 1
	}
	oldManifest, err := manifest.Load(filepath.Join(cache.Dir(root, o.id), "a2amodule.yml"))
	if err != nil {
		oldEntry, ok := l.Dependencies[o.id]
		if !ok {
			fmt.Fprintf(a.Err, "set: old manifest unavailable and dependency %s is not locked\n", o.id)
			return 1
		}
		oldRes, fetchErr := f.Fetch(a.context(), oldEntry.Git, oldEntry.Commit, defaultPath(oldEntry.Path), filepath.Join(work, "old"))
		if fetchErr != nil {
			fmt.Fprintf(a.Err, "set: restore old manifest from lock: %v\n", fetchErr)
			return 1
		}
		oldManifest, err = manifest.Parse(oldRes.Manifest)
		if err != nil {
			fmt.Fprintf(a.Err, "set: old locked manifest: %v\n", err)
			return 1
		}
	}
	sum := sha256.Sum256(res.Manifest)
	locked := manifest.LockedDependency{Git: next.Git, Ref: next.Ref, Path: defaultPath(next.Path), Commit: res.Commit, Manifest: "sha256:" + hex.EncodeToString(sum[:])}
	preflight, err := os.MkdirTemp("", "git-a2a-preflight-")
	if err != nil {
		fmt.Fprintf(a.Err, "set: %v\n", err)
		return 1
	}
	defer os.RemoveAll(preflight)
	copyAdapterFiles(root, preflight)
	if _, err = rewireSet(a.context(), preflight, oldDep, next, oldManifest, nextManifest, locked, false); err != nil {
		fmt.Fprintf(a.Err, "set: adapter cannot express change: %v; no files changed\n", err)
		return 1
	}
	if o.dry {
		fmt.Fprintf(a.Out, "%s: %s@%s -> %s@%s (%s)\n", o.id, oldDep.Git, oldDep.Ref, next.Git, next.Ref, short(res.Commit))
		fmt.Fprintln(a.Err, "set dry run: no files changed")
		if resolution.Ambiguous {
			fmt.Fprintf(a.Err, "ref %s is ambiguous; selected %s\n", next.Ref, resolution.FullRef)
		}
		return 0
	}
	snapshots := snapshotAdapterFiles(root)
	outcomes, err := rewireSet(a.context(), root, oldDep, next, oldManifest, nextManifest, locked, true)
	if err != nil {
		restoreAdapterFiles(root, snapshots)
		fmt.Fprintf(a.Err, "set: wiring failed and was rolled back: %v\n", err)
		return 1
	}
	stagedRoot := filepath.Join(work, "staged-cache")
	if err = cache.Save(stagedRoot, next.ID, res.Manifest, res.Commit, res.Method); err != nil {
		restoreAdapterFiles(root, snapshots)
		fmt.Fprintf(a.Err, "set: stage cache: %v\n", err)
		return 1
	}
	cards, cardWarnings := a.snapshotCardsTo(filepath.Join(cache.Dir(stagedRoot, next.ID), "cards"), next.Git, next.Path, res.Commit, nextManifest, f)
	locked.Cards = cards
	oldManifestBytes, _ := os.ReadFile(filepath.Join(root, "a2amodule.yml"))
	oldLockBytes, _ := os.ReadFile(filepath.Join(root, "a2amodule.lock"))
	own.Dependencies[idx] = next
	delete(l.Dependencies, o.id)
	l.Dependencies[next.ID] = locked
	if err = writeManifest(root, own); err == nil {
		err = lockfile.Write(root, l)
	}
	if err != nil {
		_ = lockfile.Atomic(filepath.Join(root, "a2amodule.yml"), oldManifestBytes, 0o644)
		if len(oldLockBytes) > 0 {
			_ = lockfile.Atomic(filepath.Join(root, "a2amodule.lock"), oldLockBytes, 0o644)
		} else {
			_ = os.Remove(filepath.Join(root, "a2amodule.lock"))
		}
		restoreAdapterFiles(root, snapshots)
		fmt.Fprintf(a.Err, "set: metadata write failed and was rolled back: %v\n", err)
		return 1
	}
	if err = replaceCache(root, next.ID, cache.Dir(stagedRoot, next.ID), work); err != nil {
		_ = lockfile.Atomic(filepath.Join(root, "a2amodule.yml"), oldManifestBytes, 0o644)
		if len(oldLockBytes) > 0 {
			_ = lockfile.Atomic(filepath.Join(root, "a2amodule.lock"), oldLockBytes, 0o644)
		} else {
			_ = os.Remove(filepath.Join(root, "a2amodule.lock"))
		}
		restoreAdapterFiles(root, snapshots)
		fmt.Fprintf(a.Err, "set: cache replacement failed and was rolled back: %v\n", err)
		return 1
	}
	if next.ID != o.id {
		_ = os.RemoveAll(cache.Dir(root, o.id))
	}
	fmt.Fprintf(a.Err, "set %s to %s at %s\n", next.ID, next.Git, res.Commit)
	if resolution.Ambiguous {
		fmt.Fprintf(a.Err, "ref %s is ambiguous; selected %s\n", next.Ref, resolution.FullRef)
	}
	for _, outcome := range outcomes {
		if outcome.Changed {
			fmt.Fprintf(a.Out, "%s: rewired %s\n", outcome.Ecosystem, next.ID)
		} else if !outcome.Wired {
			fmt.Fprintf(a.Err, "%s: not wired: %s\n", outcome.Ecosystem, outcome.Reason)
		}
	}
	for _, warning := range cardWarnings {
		fmt.Fprintf(a.Err, "warning: card snapshot: %v\n", warning)
	}
	return 0
}

func replaceCache(root, id, staged, work string) error {
	target := cache.Dir(root, id)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	backup := filepath.Join(work, "previous-cache")
	hadPrevious := false
	if _, err := os.Stat(target); err == nil {
		if err = os.Rename(target, backup); err != nil {
			return err
		}
		hadPrevious = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(staged, target); err != nil {
		if hadPrevious {
			_ = os.Rename(backup, target)
		}
		return err
	}
	return nil
}

func rewireSet(ctx context.Context, root string, oldDep, newDep manifest.Dependency, oldM, newM *manifest.Manifest, locked manifest.LockedDependency, refresh bool) ([]wireOutcome, error) {
	var outcomes []wireOutcome
	for _, impl := range adapters.All() {
		ok, _, err := impl.Detect(root)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		oldExports := exportsFor(oldM, impl.Ecosystem())
		newExports := exportsFor(newM, impl.Ecosystem())
		if !selected(newDep, impl.Ecosystem()) {
			continue
		}
		if oldDep.ID != newDep.ID {
			for _, exp := range oldExports {
				if _, err := impl.Unwire(ctx, root, oldDep, exp); err != nil {
					return nil, err
				}
			}
		}
		for _, exp := range newExports {
			change, err := impl.Wire(ctx, root, newDep, exp, locked)
			if err != nil {
				if newDep.Wire == nil && adapter.IsNotWirable(err) {
					outcomes = append(outcomes, wireOutcome{Ecosystem: impl.Ecosystem(), Reason: adapter.NotWirableReason(err)})
					continue
				}
				return nil, err
			}
			outcomes = append(outcomes, wireOutcome{Ecosystem: impl.Ecosystem(), Changed: change.Changed, Wired: true})
			if change.Changed {
				if refresh {
					if err = impl.Refresh(ctx, root, newDep, exp, locked); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	return outcomes, nil
}
func exportsFor(m *manifest.Manifest, eco string) []adapter.Export {
	var out []adapter.Export
	if m == nil {
		return out
	}
	for _, exp := range m.Module.Exports {
		if exp.Ecosystem == eco {
			out = append(out, exp)
		}
	}
	return out
}
func selected(dep manifest.Dependency, eco string) bool {
	if dep.Wire == nil {
		return true
	}
	for _, v := range *dep.Wire {
		if v == eco {
			return true
		}
	}
	return false
}
func appendUnique(values []string, value string) []string {
	for _, v := range values {
		if v == value {
			return values
		}
	}
	return append(values, value)
}

var adapterFiles = []string{"package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "bun.lock", "bun.lockb", "pyproject.toml", "uv.lock", "poetry.lock", "pdm.lock", "go.mod", "go.sum", ".yarnrc.yml"}

func copyAdapterFiles(from, to string) {
	_ = os.MkdirAll(to, 0o755)
	for _, name := range adapterFiles {
		if b, err := os.ReadFile(filepath.Join(from, name)); err == nil {
			_ = os.WriteFile(filepath.Join(to, name), b, 0o644)
		}
	}
}
func snapshotAdapterFiles(root string) map[string][]byte {
	out := map[string][]byte{}
	for _, name := range adapterFiles {
		if b, err := os.ReadFile(filepath.Join(root, name)); err == nil {
			out[name] = b
		} else {
			out[name] = nil
		}
	}
	return out
}
func restoreAdapterFiles(root string, snapshots map[string][]byte) {
	for name, b := range snapshots {
		p := filepath.Join(root, name)
		if b == nil {
			_ = os.Remove(p)
		} else {
			_ = os.WriteFile(p, b, 0o644)
		}
	}
}
func isHex(s string) bool {
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}
