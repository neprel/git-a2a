// Package lifecycle owns the five-command component dependency lifecycle.
package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/neprel/git-a2a/v2/adapters"
	"github.com/neprel/git-a2a/v2/internal/adapter"
	"github.com/neprel/git-a2a/v2/internal/cache"
	"github.com/neprel/git-a2a/v2/internal/cardmetadata"
	"github.com/neprel/git-a2a/v2/internal/fetch"
	"github.com/neprel/git-a2a/v2/internal/gitx"
	lockfile "github.com/neprel/git-a2a/v2/internal/lock"
	"github.com/neprel/git-a2a/v2/internal/manifest"
)

type Service struct {
	Root     string
	Runner   gitx.Runner
	Adapters []adapter.Adapter
}

type Result struct {
	Name    string `json:"name"`
	Commit  string `json:"commit,omitempty"`
	Changed bool   `json:"changed"`
	Warning string `json:"warning,omitempty"`
	Error   string `json:"error,omitempty"`
}

type ListItem struct {
	Name      string                `json:"name"`
	Component string                `json:"component,omitempty"`
	Git       string                `json:"git"`
	Ref       string                `json:"ref,omitempty"`
	Commit    string                `json:"commit,omitempty"`
	Bindings  []manifest.Binding    `json:"bindings"`
	Agent     *manifest.LockedAgent `json:"agent,omitempty"`
	Surface   string                `json:"surface,omitempty"`
	Problems  []string              `json:"problems,omitempty"`
}

func (s Service) root() string {
	if s.Root == "" {
		return "."
	}
	return s.Root
}
func (s Service) runner() gitx.Runner {
	if s.Runner != nil {
		return s.Runner
	}
	return gitx.ExecRunner{Timeout: 120 * time.Second}
}
func (s Service) implementations() []adapter.Adapter {
	if s.Adapters != nil {
		return s.Adapters
	}
	return adapters.All()
}

func (s Service) Init(id, description string) error {
	root := s.root()
	if _, err := manifest.Path(root); err == nil {
		return fmt.Errorf("manifest already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if id == "" {
		id = sanitize(filepath.Base(mustAbs(root)))
	}
	m := &manifest.Manifest{Schema: manifest.CurrentSchema, Component: manifest.Component{ID: id, Description: description}}
	if err := m.Validate(); err != nil {
		return err
	}
	b, err := manifest.Marshal(m)
	if err != nil {
		return err
	}
	ignorePath := filepath.Join(root, ".gitignore")
	ignoreBefore, ignoreErr := os.ReadFile(ignorePath)
	ignoreExisted := ignoreErr == nil
	if ignoreErr != nil && !os.IsNotExist(ignoreErr) {
		return ignoreErr
	}
	if info, lstatErr := os.Lstat(ignorePath); lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf(".gitignore must be a regular file, not a symlink")
	} else if lstatErr != nil && !os.IsNotExist(lstatErr) {
		return lstatErr
	}
	if err := lockfile.Atomic(filepath.Join(root, manifest.CanonicalName), b, 0o644); err != nil {
		return err
	}
	if err := ensureIgnored(root); err != nil {
		manifestErr := os.Remove(filepath.Join(root, manifest.CanonicalName))
		var ignoreRestoreErr error
		if ignoreExisted {
			ignoreRestoreErr = lockfile.Atomic(ignorePath, ignoreBefore, 0o644)
		} else {
			ignoreRestoreErr = os.Remove(ignorePath)
			if os.IsNotExist(ignoreRestoreErr) {
				ignoreRestoreErr = nil
			}
		}
		return withRollback(err, manifestErr, ignoreRestoreErr)
	}
	return nil
}

func (s Service) Add(ctx context.Context, source, name, ref, modulePath string) (Result, error) {
	root := s.root()
	own, err := manifest.LoadDir(root)
	if err != nil {
		return Result{}, fmt.Errorf("own manifest: %w", err)
	}
	currentLock, err := lockfile.Load(root)
	if err != nil {
		return Result{}, fmt.Errorf("own lock: %w", err)
	}
	if modulePath == "" {
		modulePath = "."
	}
	work, err := os.MkdirTemp("", "git-a2a-add-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(work)
	res, upstream, err := s.fetchUpstream(ctx, source, ref, modulePath, work)
	if err != nil {
		return Result{}, err
	}
	if upstream.Agent == nil || upstream.Agent.Card == "" {
		return Result{}, fmt.Errorf("upstream component %s does not declare agent.card", upstream.Component.ID)
	}
	if own.Component.ID == upstream.Component.ID || own.Component.Repository != "" && gitx.NormalizeURL(own.Component.Repository) == gitx.NormalizeURL(source) {
		return Result{}, fmt.Errorf("self-reference to component %s", upstream.Component.ID)
	}
	if name == "" {
		name = upstream.Component.ID
	}
	if existing, _ := manifest.DependencyByName(own, name); existing != nil {
		return Result{}, fmt.Errorf("dependency %s already exists", name)
	}
	bindings, fallbackWarning, err := s.selectBindings(root, name, source, cleanModulePath(modulePath), upstream)
	if err != nil {
		return Result{}, err
	}
	requestedRef := ref
	if requestedRef == "" {
		requestedRef = strings.TrimPrefix(res.Ref, "refs/heads/")
		if requestedRef == res.Ref {
			requestedRef = res.Ref
		}
	}
	dep := manifest.Dependency{Name: name, Git: source, Ref: requestedRef, Path: cleanModulePath(modulePath), Bindings: bindings}
	prospectiveManifest := cloneManifest(own)
	prospectiveManifest.Dependencies = append(prospectiveManifest.Dependencies, dep)
	manifest.Sort(prospectiveManifest)
	if err = prospectiveManifest.Validate(); err != nil {
		return Result{}, fmt.Errorf("prospective manifest: %w", err)
	}
	preparedCard, err := cardmetadata.Prepare(ctx, fetch.Fetcher{Runner: s.runner()}, dep.Name, dep.Git, res.Commit, defaultPath(dep.Path), *upstream.Agent)
	if err != nil {
		return Result{}, err
	}
	locked := lockedFrom(dep, upstream, res, preparedCard.Agent)
	prospectiveLock := cloneLock(currentLock)
	prospectiveLock.Dependencies[name] = locked
	if err = validateState(prospectiveManifest, prospectiveLock); err != nil {
		return Result{}, err
	}
	if err = s.preflightNewBindings(ctx, dep, locked); err != nil {
		return Result{}, err
	}
	tx, err := s.applyTransaction(ctx, dep, upstream, locked, nil, preparedCard, work)
	if err != nil {
		return Result{}, err
	}
	beforeManifest, manifestPath, _ := manifest.ReadDir(root)
	beforeLock, _ := os.ReadFile(filepath.Join(root, "a2amodule.lock"))
	if err = writeState(root, prospectiveManifest, prospectiveLock); err != nil {
		return Result{}, withRollback(err,
			restoreState(manifestPath, beforeManifest, filepath.Join(root, "a2amodule.lock"), beforeLock),
			tx.rollback())
	}
	if err = cache.SaveAs(root, name, res.Manifest, res.Commit, res.Method, res.ManifestName); err != nil {
		return Result{}, withRollback(fmt.Errorf("cache: %w", err),
			restoreState(manifestPath, beforeManifest, filepath.Join(root, "a2amodule.lock"), beforeLock),
			tx.rollback())
	}
	tx.finalize()
	return Result{Name: name, Commit: res.Commit, Changed: true, Warning: fallbackWarning}, nil
}

func (s Service) Pull(ctx context.Context, only string) ([]Result, error) {
	root := s.root()
	own, err := manifest.LoadDir(root)
	if err != nil {
		return nil, err
	}
	var deps []manifest.Dependency
	for _, d := range own.Dependencies {
		if only == "" || d.Name == only {
			deps = append(deps, d)
		}
	}
	if len(deps) == 0 {
		return nil, fmt.Errorf("dependency %q not found", only)
	}
	sort.Slice(deps, func(i, j int) bool { return deps[i].Name < deps[j].Name })
	// Common read-only preflight before any dependency mutates.
	currentLock, lockErr := lockfile.Load(root)
	if lockErr != nil {
		return nil, lockErr
	}
	for _, d := range deps {
		if err = s.preflightBindings(ctx, d); err != nil {
			return nil, fmt.Errorf("%s: %w", d.Name, err)
		}
		entry, ok := currentLock.Dependencies[d.Name]
		if !ok {
			continue
		}
		for _, b := range d.Bindings {
			if err = s.preflightDrift(ctx, d, b, entry); err != nil {
				return nil, fmt.Errorf("%s: %w", d.Name, err)
			}
		}
	}
	var results []Result
	var joined []error
	for _, d := range deps {
		result, e := s.pullOne(ctx, d)
		results = append(results, result)
		if e != nil {
			results[len(results)-1].Error = e.Error()
			joined = append(joined, fmt.Errorf("%s: %w", d.Name, e))
		}
	}
	return results, errors.Join(joined...)
}

func (s Service) pullOne(ctx context.Context, dep manifest.Dependency) (Result, error) {
	root := s.root()
	own, err := manifest.LoadDir(root)
	if err != nil {
		return Result{Name: dep.Name}, err
	}
	work, err := os.MkdirTemp("", "git-a2a-pull-")
	if err != nil {
		return Result{Name: dep.Name}, err
	}
	defer os.RemoveAll(work)
	res, upstream, err := s.fetchUpstream(ctx, dep.Git, dep.Ref, defaultPath(dep.Path), work)
	if err != nil {
		return Result{Name: dep.Name}, err
	}
	if upstream.Agent == nil || upstream.Agent.Card == "" {
		return Result{Name: dep.Name}, fmt.Errorf("upstream component %s does not declare agent.card", upstream.Component.ID)
	}
	l, err := lockfile.Load(root)
	if err != nil {
		return Result{Name: dep.Name}, err
	}
	old, hadOld := l.Dependencies[dep.Name]
	if hadOld && old.Component != "" && old.Component != upstream.Component.ID {
		return Result{Name: dep.Name}, fmt.Errorf("upstream identity changed from %s to %s", old.Component, upstream.Component.ID)
	}
	if len(dep.Bindings) == 0 {
		dep.Bindings, _, err = s.selectBindings(root, dep.Name, dep.Git, dep.Path, upstream)
		if err != nil {
			return Result{Name: dep.Name}, err
		}
		if declared, idx := manifest.DependencyByName(own, dep.Name); declared != nil {
			own.Dependencies[idx].Bindings = dep.Bindings
		}
	}
	preparedCard, err := cardmetadata.Prepare(ctx, fetch.Fetcher{Runner: s.runner()}, dep.Name, dep.Git, res.Commit, defaultPath(dep.Path), *upstream.Agent)
	if err != nil {
		return Result{Name: dep.Name}, err
	}
	locked := lockedFrom(dep, upstream, res, preparedCard.Agent)
	prospectiveManifest := cloneManifest(own)
	if _, idx := manifest.DependencyByName(prospectiveManifest, dep.Name); idx >= 0 {
		prospectiveManifest.Dependencies[idx] = dep
	}
	prospectiveLock := cloneLock(l)
	prospectiveLock.Dependencies[dep.Name] = locked
	if err = validateState(prospectiveManifest, prospectiveLock); err != nil {
		return Result{Name: dep.Name}, err
	}
	tx, err := s.applyTransaction(ctx, dep, upstream, locked, func() *manifest.LockedDependency {
		if hadOld {
			return &old
		}
		return nil
	}(), preparedCard, work)
	if err != nil {
		return Result{Name: dep.Name}, err
	}
	beforeManifest, manifestPath, _ := manifest.ReadDir(root)
	beforeLock, _ := os.ReadFile(filepath.Join(root, "a2amodule.lock"))
	if err = writeState(root, prospectiveManifest, prospectiveLock); err != nil {
		return Result{Name: dep.Name}, withRollback(err,
			restoreState(manifestPath, beforeManifest, filepath.Join(root, "a2amodule.lock"), beforeLock),
			tx.rollback())
	}
	if err = cache.SaveAs(root, dep.Name, res.Manifest, res.Commit, res.Method, res.ManifestName); err != nil {
		return Result{Name: dep.Name}, withRollback(fmt.Errorf("cache: %w", err),
			restoreState(manifestPath, beforeManifest, filepath.Join(root, "a2amodule.lock"), beforeLock),
			tx.rollback())
	}
	tx.finalize()
	return Result{Name: dep.Name, Commit: res.Commit, Changed: !hadOld || old.Commit != res.Commit}, nil
}

func (s Service) Remove(ctx context.Context, name string) (Result, error) {
	root := s.root()
	own, err := manifest.LoadDir(root)
	if err != nil {
		return Result{}, err
	}
	dep, idx := manifest.DependencyByName(own, name)
	if dep == nil {
		return Result{}, fmt.Errorf("dependency %q not found", name)
	}
	l, err := lockfile.Load(root)
	if err != nil {
		return Result{}, err
	}
	locked, ok := l.Dependencies[name]
	if !ok {
		for _, binding := range dep.Bindings {
			if binding.Adapter == "submodule" {
				return Result{}, fmt.Errorf("dependency %s has no lock entry; run git a2a pull %s before removing its submodule", name, name)
			}
		}
		locked = manifest.LockedDependency{Git: dep.Git, Ref: dep.Ref, Path: dep.Path, Bindings: dep.Bindings}
	}
	snap := snapshotAdapterFiles(root)
	if err = s.preflightBindings(ctx, *dep); err != nil {
		return Result{}, err
	}
	if ok {
		for _, b := range dep.Bindings {
			if err = s.preflightDrift(ctx, *dep, b, locked); err != nil {
				return Result{}, err
			}
		}
	}
	cacheRollback, cacheFinalize, err := stageDirectoryRemoval(cache.Dir(root, name))
	if err != nil {
		return Result{}, fmt.Errorf("stage cache removal: %w", err)
	}
	surfaceRollback, surfaceFinalize, err := stageDirectoryRemoval(filepath.Join(root, ".git-a2a", "surfaces", name))
	if err != nil {
		return Result{}, withRollback(fmt.Errorf("stage surface removal: %w", err), cacheRollback())
	}
	cardRollback, cardFinalize, err := cardmetadata.Remove(root, name)
	if err != nil {
		return Result{}, withRollback(fmt.Errorf("stage agent card removal: %w", err), surfaceRollback(), cacheRollback())
	}
	var removed []manifest.Binding
	rollbackRemoved := func() error {
		var failures []error
		for i := len(removed) - 1; i >= 0; i-- {
			b := removed[i]
			impl := s.implementationFor(b)
			exp := exportFromBinding(*dep, b)
			_, restoreErr := impl.Pull(ctx, root, *dep, exp, locked)
			if restoreErr != nil {
				failures = append(failures, fmt.Errorf("restore %s/%s: %w", b.Adapter, b.Variant, restoreErr))
			}
		}
		failures = append(failures, restoreAdapterFiles(root, snap))
		return errors.Join(failures...)
	}
	for i := len(dep.Bindings) - 1; i >= 0; i-- {
		b := dep.Bindings[i]
		impl := s.implementationFor(b)
		exp := exportFromBinding(*dep, b)
		// Include the current binding before invoking the manager: a manager may
		// edit its declaration/lock/materialization and then fail.
		removed = append(removed, b)
		if _, err = impl.Remove(ctx, root, *dep, exp, locked); err != nil {
			return Result{}, withRollback(fmt.Errorf("remove %s: %w", b.Adapter, err), rollbackRemoved(), cardRollback(), surfaceRollback(), cacheRollback())
		}
	}
	beforeManifest, manifestPath, _ := manifest.ReadDir(root)
	beforeLock, _ := os.ReadFile(filepath.Join(root, "a2amodule.lock"))
	own.Dependencies = append(own.Dependencies[:idx], own.Dependencies[idx+1:]...)
	delete(l.Dependencies, name)
	if err = writeState(root, own, l); err != nil {
		return Result{}, withRollback(err, cardRollback(), surfaceRollback(), cacheRollback(), rollbackRemoved(), restoreState(manifestPath, beforeManifest, filepath.Join(root, "a2amodule.lock"), beforeLock))
	}
	cacheFinalize()
	surfaceFinalize()
	cardFinalize()
	_ = locked
	return Result{Name: name, Changed: true}, nil
}

func stageDirectoryRemoval(target string) (rollback func() error, finalize func(), err error) {
	if _, err = os.Stat(target); os.IsNotExist(err) {
		return func() error { return nil }, func() {}, nil
	} else if err != nil {
		return nil, nil, err
	}
	parent := filepath.Dir(target)
	backup, err := os.MkdirTemp(parent, "."+filepath.Base(target)+"-remove-")
	if err != nil {
		return nil, nil, err
	}
	if err = os.Remove(backup); err != nil {
		return nil, nil, err
	}
	if err = os.Rename(target, backup); err != nil {
		return nil, nil, err
	}
	return func() error {
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		return os.Rename(backup, target)
	}, func() { _ = os.RemoveAll(backup) }, nil
}

func (s Service) List(name string) ([]ListItem, error) {
	root := s.root()
	own, err := manifest.LoadDir(root)
	if err != nil {
		return nil, err
	}
	l, lockErr := lockfile.Load(root)
	if lockErr != nil && !os.IsNotExist(lockErr) {
		return nil, lockErr
	}
	var out []ListItem
	for _, d := range own.Dependencies {
		if name != "" && d.Name != name {
			continue
		}
		item := ListItem{Name: d.Name, Git: d.Git, Ref: d.Ref, Bindings: append([]manifest.Binding(nil), d.Bindings...)}
		var applied *manifest.LockedDependency
		if l != nil {
			if x, ok := l.Dependencies[d.Name]; ok {
				lockedCopy := x
				applied = &lockedCopy
				item.Component = x.Component
				item.Commit = x.Commit
				listedAgent, cardProblems := cardmetadata.ForList(root, d.Name, x.Agent)
				item.Agent = &listedAgent
				item.Problems = append(item.Problems, cardProblems...)
				item.Surface = x.Surface
				if x.Git != d.Git || x.Ref != d.Ref {
					item.Problems = append(item.Problems, "declaration differs from lock")
				}
			} else {
				item.Problems = append(item.Problems, "not installed; run git a2a pull "+d.Name)
			}
		}
		for _, b := range d.Bindings {
			impl := s.implementationFor(b)
			if impl == nil {
				item.Problems = append(item.Problems, fmt.Sprintf("adapter unavailable: %s/%s", b.Adapter, b.Variant))
				continue
			}
			if b.Adapter != "submodule" {
				ok, v, e := impl.Detect(root)
				if e != nil {
					item.Problems = append(item.Problems, e.Error())
				} else if !ok {
					item.Problems = append(item.Problems, "consumer no longer matches adapter "+b.Adapter)
				} else if string(v) != b.Variant {
					item.Problems = append(item.Problems, fmt.Sprintf("adapter %s variant is %s, saved %s", b.Adapter, v, b.Variant))
				}
			}
			if applied != nil {
				findings, driftErr := impl.Inspect(context.Background(), root, d, exportFromBinding(d, b), *applied)
				if driftErr != nil {
					item.Problems = append(item.Problems, fmt.Sprintf("%s/%s inspect: %v", b.Adapter, b.Variant, driftErr))
				} else {
					for _, finding := range findings {
						got := finding.Got
						if got == "" {
							got = "missing"
						}
						item.Problems = append(item.Problems, fmt.Sprintf("%s/%s %s/%s: want %s, got %s", b.Adapter, b.Variant, finding.File, finding.Entry, finding.Want, got))
					}
				}
			}
		}
		out = append(out, item)
	}
	if name != "" && len(out) == 0 {
		return nil, fmt.Errorf("dependency %q not found", name)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s Service) fetchUpstream(ctx context.Context, source, ref, modulePath, work string) (fetch.Result, *manifest.Manifest, error) {
	res, err := (fetch.Fetcher{Runner: s.runner()}).Fetch(ctx, source, ref, modulePath, filepath.Join(work, "fetch"))
	if err != nil {
		return res, nil, err
	}
	m, err := manifest.Parse(res.Manifest)
	if err != nil {
		return res, nil, fmt.Errorf("upstream manifest: %w", err)
	}
	return res, m, nil
}

var buildAdapters = map[string]bool{"cmake": true, "nuget": true, "maven": true, "meson": true}

func (s Service) selectBindings(root, name, source, modulePath string, upstream *manifest.Manifest) ([]manifest.Binding, string, error) {
	var out []manifest.Binding
	needsSource := false
	fallbackReason := ""
	for _, exp := range upstream.Component.Exports {
		for _, impl := range s.implementationsFor(exp.Adapter) {
			ok, v, err := impl.Detect(root)
			if err != nil {
				return nil, "", err
			}
			if !ok {
				continue
			}
			b := manifest.Binding{Adapter: exp.Adapter, Variant: string(v), Export: exp.Name, Path: exp.Path}
			if buildAdapters[exp.Adapter] {
				needsSource = true
				b.Path = filepath.ToSlash(filepath.Join("deps", name, defaultExportPath(modulePath), defaultExportPath(exp.Path)))
			}
			capabilityExport := exp
			capabilityExport.Path = b.Path
			if err = impl.Capability(root, manifest.Dependency{Name: name, Git: source, Path: modulePath}, capabilityExport); err != nil {
				if adapter.IsNotWirable(err) {
					needsSource = true
					fallbackReason = adapter.NotWirableReason(err)
					continue
				}
				return nil, "", err
			}
			out = append(out, b)
		}
	}
	if len(out) == 0 || needsSource {
		submoduleBinding := manifest.Binding{Adapter: "submodule", Variant: "git", Export: name, Path: filepath.ToSlash(filepath.Join("deps", name))}
		submoduleAdapter := s.implementationFor(submoduleBinding)
		if submoduleAdapter == nil {
			return nil, "", fmt.Errorf("no native binding applies and submodule adapter is unavailable")
		}
		if err := submoduleAdapter.Capability(root, manifest.Dependency{Name: name, Git: source, Path: modulePath}, exportFromBinding(manifest.Dependency{Name: name}, submoduleBinding)); err != nil {
			return nil, "", err
		}
		out = append([]manifest.Binding{submoduleBinding}, out...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Adapter == out[j].Adapter {
			if out[i].Variant == out[j].Variant {
				return out[i].Export < out[j].Export
			}
			return out[i].Variant < out[j].Variant
		}
		return out[i].Adapter < out[j].Adapter
	})
	for i, b := range out {
		if b.Adapter == "submodule" {
			out = append([]manifest.Binding{b}, append(out[:i], out[i+1:]...)...)
			break
		}
	}
	warning := ""
	if fallbackReason != "" {
		warning = fallbackReason + "; sources were materialized with submodule fallback without automatic native integration"
	}
	return out, warning, nil
}

func (s Service) preflightBindings(ctx context.Context, dep manifest.Dependency) error {
	for _, b := range dep.Bindings {
		impl := s.implementationFor(b)
		if impl == nil {
			return fmt.Errorf("saved adapter %s variant %s is unavailable", b.Adapter, b.Variant)
		}
		exp := exportFromBinding(dep, b)
		if err := impl.Capability(s.root(), dep, exp); err != nil {
			return fmt.Errorf("saved adapter %s/%s capability: %w; refusing to switch adapter", b.Adapter, b.Variant, err)
		}
		if b.Adapter == "submodule" {
			continue
		}
		if err := adapter.RequireTool(ctx, b.Adapter, adapter.Variant(b.Variant)); err != nil {
			return err
		}
		ok, v, err := impl.Detect(s.root())
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("saved adapter %s no longer applies", b.Adapter)
		}
		if string(v) != b.Variant {
			return fmt.Errorf("saved adapter %s variant %s is unavailable (detected %s); refusing to switch", b.Adapter, b.Variant, v)
		}
	}
	return nil
}

func (s Service) preflightDrift(ctx context.Context, dep manifest.Dependency, binding manifest.Binding, locked manifest.LockedDependency) error {
	impl := s.implementationFor(binding)
	if impl == nil {
		return fmt.Errorf("saved adapter %s variant %s is unavailable", binding.Adapter, binding.Variant)
	}
	findings, err := impl.Inspect(ctx, s.root(), dep, exportFromBinding(dep, binding), locked)
	if err != nil {
		return fmt.Errorf("preflight %s/%s: %w", binding.Adapter, binding.Variant, err)
	}
	for _, finding := range findings {
		if !finding.Repairable && finding.Got != "" && finding.Got != "missing" {
			return fmt.Errorf("preflight %s/%s: %s/%s: want %s, got %s", binding.Adapter, binding.Variant, finding.File, finding.Entry, finding.Want, finding.Got)
		}
	}
	return nil
}

func (s Service) preflightNewBindings(ctx context.Context, dep manifest.Dependency, locked manifest.LockedDependency) error {
	for _, binding := range dep.Bindings {
		impl := s.implementationFor(binding)
		if impl == nil {
			return fmt.Errorf("saved adapter %s variant %s is unavailable", binding.Adapter, binding.Variant)
		}
		findings, err := impl.Inspect(ctx, s.root(), dep, exportFromBinding(dep, binding), locked)
		if err != nil {
			return fmt.Errorf("preflight %s/%s: %w", binding.Adapter, binding.Variant, err)
		}
		declarationMissing := false
		for _, finding := range findings {
			if finding.Got == "" || binding.Adapter == "submodule" && finding.Entry == dep.Name && finding.Got == "missing" {
				declarationMissing = true
			}
		}
		if !declarationMissing {
			return fmt.Errorf("preflight %s/%s: integration entry %s already exists; refusing to claim ownership", binding.Adapter, binding.Variant, binding.Export)
		}
	}
	return nil
}

type transaction struct {
	rollback func() error
	finalize func()
}

func (s Service) applyTransaction(ctx context.Context, dep manifest.Dependency, upstream *manifest.Manifest, locked manifest.LockedDependency, old *manifest.LockedDependency, preparedCard cardmetadata.Prepared, work string) (*transaction, error) {
	root := s.root()
	exports := make(map[string]manifest.Export, len(dep.Bindings))
	for _, b := range dep.Bindings {
		exp, ok := findExport(upstream, b)
		if b.Adapter == "submodule" {
			exp, ok = exportFromBinding(dep, b), true
		}
		if !ok {
			return nil, fmt.Errorf("saved export %s/%s is no longer declared", b.Adapter, b.Export)
		}
		if b.Adapter != "submodule" && !buildAdapters[b.Adapter] && cleanModulePath(exp.Path) != cleanModulePath(b.Path) {
			impl := s.implementationFor(b)
			if impl == nil {
				return nil, fmt.Errorf("saved adapter %s/%s is unavailable", b.Adapter, b.Variant)
			}
			if capabilityErr := impl.Capability(root, dep, exp); capabilityErr != nil {
				return nil, fmt.Errorf("saved adapter %s/%s no longer supports upstream source: %w; refusing to switch adapter", b.Adapter, b.Variant, capabilityErr)
			}
			return nil, fmt.Errorf("saved export %s/%s path changed from %q to %q; refusing to retarget automatically", b.Adapter, b.Export, b.Path, exp.Path)
		}
		if buildAdapters[b.Adapter] {
			exp.Path = b.Path
		}
		exports[bindingKey(b)] = exp
	}
	if err := s.preflightBindings(ctx, dep); err != nil {
		return nil, err
	}
	for _, b := range dep.Bindings {
		inspectLocked := locked
		if old != nil {
			inspectLocked = *old
		}
		if err := s.preflightDrift(ctx, dep, b, inspectLocked); err != nil {
			return nil, err
		}
	}
	snap := snapshotAdapterFiles(root)
	var applied []manifest.Binding
	rollbackBindings := func() error {
		var failures []error
		for i := len(applied) - 1; i >= 0; i-- {
			b := applied[i]
			impl := s.implementationFor(b)
			exp := exports[bindingKey(b)]
			if old == nil {
				if _, err := impl.Remove(ctx, root, dep, exp, locked); err != nil {
					failures = append(failures, fmt.Errorf("remove %s/%s: %w", b.Adapter, b.Variant, err))
				}
				continue
			}
			_, restoreErr := impl.Pull(ctx, root, dep, exp, *old)
			if restoreErr != nil {
				failures = append(failures, fmt.Errorf("restore %s/%s: %w", b.Adapter, b.Variant, restoreErr))
			}
		}
		failures = append(failures, restoreAdapterFiles(root, snap))
		return errors.Join(failures...)
	}
	for _, b := range dep.Bindings {
		impl := s.implementationFor(b)
		exp := exports[bindingKey(b)]
		// Pull may mutate native state before a manager error. Track it first so
		// rollback can run the inverse/previous Pull in addition to file restore.
		applied = append(applied, b)
		change, e := impl.Pull(ctx, root, dep, exp, locked)
		if e != nil {
			return nil, withRollback(fmt.Errorf("%s: %w", b.Adapter, e), rollbackBindings())
		}
		_ = change
		findings, inspectErr := impl.Inspect(ctx, root, dep, exp, locked)
		if inspectErr != nil || len(findings) != 0 {
			if inspectErr == nil {
				inspectErr = fmt.Errorf("dependency is not usable after pull: %v", findings)
			}
			return nil, withRollback(fmt.Errorf("%s: %w", b.Adapter, inspectErr), rollbackBindings())
		}
	}
	cardRollback, cardFinalize, e := cardmetadata.Apply(root, dep.Name, preparedCard)
	if e != nil {
		return nil, withRollback(fmt.Errorf("agent card: %w", e), rollbackBindings())
	}
	surfaceRollback, surfaceFinalize, e := s.materializeSurface(ctx, dep, upstream, locked, work)
	if e != nil {
		return nil, withRollback(e, cardRollback(), rollbackBindings())
	}
	return &transaction{
		rollback: func() error { return errors.Join(surfaceRollback(), cardRollback(), rollbackBindings()) },
		finalize: func() { surfaceFinalize(); cardFinalize() },
	}, nil
}

func bindingKey(b manifest.Binding) string { return b.Adapter + "\x00" + b.Variant + "\x00" + b.Export }

func (s Service) materializeSurface(ctx context.Context, dep manifest.Dependency, upstream *manifest.Manifest, locked manifest.LockedDependency, work string) (func() error, func(), error) {
	target := filepath.Join(s.root(), ".git-a2a", "surfaces", dep.Name)
	backup := ""
	hadPrevious := false
	if _, err := os.Stat(target); err == nil {
		backup, err = os.MkdirTemp(filepath.Dir(target), "."+filepath.Base(target)+"-surface-backup-")
		if err != nil {
			return nil, nil, err
		}
		if err = os.Remove(backup); err != nil {
			return nil, nil, err
		}
		if err = os.Rename(target, backup); err != nil {
			return nil, nil, err
		}
		hadPrevious = true
	} else if !os.IsNotExist(err) {
		return nil, nil, err
	}
	restore := func() error {
		var failures []error
		if err := os.RemoveAll(target); err != nil {
			failures = append(failures, err)
		}
		if hadPrevious {
			if err := os.Rename(backup, target); err != nil {
				failures = append(failures, err)
			}
		}
		return errors.Join(failures...)
	}
	finalize := func() { _ = os.RemoveAll(backup) }
	if upstream.Component.Surface == "" {
		return restore, finalize, nil
	}
	stage := filepath.Join(work, "surface")
	_, err := (fetch.Fetcher{Runner: s.runner()}).Surface(ctx, dep.Git, locked.Commit, defaultPath(dep.Path), upstream.Component.Surface, stage, filepath.Join(work, "surface-work"))
	if err != nil {
		return nil, nil, withRollback(fmt.Errorf("surface: %w", err), restore())
	}
	if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, nil, withRollback(err, restore())
	}
	if err = os.Rename(stage, target); err != nil {
		return nil, nil, withRollback(err, restore())
	}
	return restore, finalize, nil
}

func lockedFrom(dep manifest.Dependency, up *manifest.Manifest, res fetch.Result, agent manifest.LockedAgent) manifest.LockedDependency {
	sum := sha256.Sum256(res.Manifest)
	surface := ""
	if up.Component.Surface != "" {
		surface = filepath.ToSlash(filepath.Join(".git-a2a", "surfaces", dep.Name))
	}
	return manifest.LockedDependency{Component: up.Component.ID, Git: dep.Git, Ref: dep.Ref, Path: dep.Path, Commit: res.Commit, Manifest: "sha256:" + hex.EncodeToString(sum[:]), Bindings: append([]manifest.Binding(nil), dep.Bindings...), Agent: agent, Surface: surface}
}
func (s Service) implementationsFor(ecosystem string) []adapter.Adapter {
	var matches []adapter.Adapter
	for _, a := range s.implementations() {
		if a.Ecosystem() == ecosystem {
			matches = append(matches, a)
		}
	}
	return matches
}

func (s Service) implementationFor(binding manifest.Binding) adapter.Adapter {
	for _, impl := range s.implementationsFor(binding.Adapter) {
		if binding.Adapter == "submodule" && binding.Variant == "git" {
			return impl
		}
		ok, variant, err := impl.Detect(s.root())
		if err == nil && ok && string(variant) == binding.Variant {
			return impl
		}
	}
	return nil
}
func findExport(m *manifest.Manifest, b manifest.Binding) (manifest.Export, bool) {
	for _, e := range m.Component.Exports {
		if e.Adapter == b.Adapter && e.Name == b.Export {
			return e, true
		}
	}
	return manifest.Export{}, false
}
func exportFromBinding(dep manifest.Dependency, b manifest.Binding) manifest.Export {
	return manifest.Export{Adapter: b.Adapter, Name: b.Export, Path: b.Path}
}

var adapterFiles = []string{"package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "bun.lock", "bun.lockb", "pyproject.toml", "uv.lock", "poetry.lock", "pdm.lock", "go.mod", "go.sum", "Cargo.toml", "Cargo.lock", "Package.swift", "Package.resolved", "pubspec.yaml", "pubspec.lock", "Gemfile", "Gemfile.lock", "composer.json", "composer.lock", "mix.exs", "mix.lock", "stack.yaml", "stack.yaml.lock", "cabal.project", "cabal.project.freeze", "build.zig.zon", "deps.edn", "flake.nix", "flake.lock", "CMakeLists.txt", "deps/git-a2a.cmake", "settings.gradle", "settings.gradle.kts", "deps/git-a2a.settings.gradle", "deps/git-a2a.settings.gradle.kts", "deps/git-a2a.targets", "pom.xml", "deps/git-a2a.maven/pom.xml", "meson.build", "deps/git-a2a/meson.build", ".gitmodules"}

func adapterFileNames(root string) []string {
	out := append([]string(nil), adapterFiles...)
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".csproj") || strings.HasSuffix(e.Name(), ".fsproj")) {
			out = append(out, e.Name())
		}
	}
	return out
}
func snapshotAdapterFiles(root string) map[string][]byte {
	out := map[string][]byte{}
	for _, n := range adapterFileNames(root) {
		b, e := os.ReadFile(filepath.Join(root, n))
		if e == nil {
			out[n] = b
		} else {
			out[n] = nil
		}
	}
	return out
}
func restoreAdapterFiles(root string, snap map[string][]byte) error {
	var failures []error
	for n, b := range snap {
		p := filepath.Join(root, n)
		if b == nil {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				failures = append(failures, fmt.Errorf("remove %s: %w", n, err))
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				failures = append(failures, fmt.Errorf("create parent for %s: %w", n, err))
				continue
			}
			if err := os.WriteFile(p, b, 0o644); err != nil {
				failures = append(failures, fmt.Errorf("restore %s: %w", n, err))
			}
		}
	}
	return errors.Join(failures...)
}
func writeState(root string, m *manifest.Manifest, l *manifest.Lock) error {
	if err := validateState(m, l); err != nil {
		return err
	}
	p, err := manifest.Path(root)
	if err != nil {
		return err
	}
	b, err := manifest.Marshal(m)
	if err != nil {
		return err
	}
	if err = lockfile.Atomic(p, b, 0o644); err != nil {
		return err
	}
	return lockfile.Write(root, l)
}

func cloneManifest(source *manifest.Manifest) *manifest.Manifest {
	copyManifest := *source
	copyManifest.Dependencies = append([]manifest.Dependency(nil), source.Dependencies...)
	for i := range copyManifest.Dependencies {
		copyManifest.Dependencies[i].Bindings = append([]manifest.Binding(nil), source.Dependencies[i].Bindings...)
	}
	return &copyManifest
}

func cloneLock(source *manifest.Lock) *manifest.Lock {
	copyLock := &manifest.Lock{Schema: source.Schema, Dependencies: make(map[string]manifest.LockedDependency, len(source.Dependencies))}
	for name, dependency := range source.Dependencies {
		dependency.Bindings = append([]manifest.Binding(nil), dependency.Bindings...)
		copyLock.Dependencies[name] = dependency
	}
	return copyLock
}

func validateState(m *manifest.Manifest, l *manifest.Lock) error {
	if err := m.Validate(); err != nil {
		return fmt.Errorf("prospective manifest: %w", err)
	}
	if err := l.Validate(); err != nil {
		return fmt.Errorf("prospective lock: %w", err)
	}
	for name, applied := range l.Dependencies {
		declared, _ := manifest.DependencyByName(m, name)
		if declared == nil {
			return fmt.Errorf("prospective state: lock dependency %s is not declared", name)
		}
		if declared.Git != applied.Git || declared.Ref != applied.Ref || declared.Path != applied.Path || !sameBindings(declared.Bindings, applied.Bindings) {
			return fmt.Errorf("prospective state: dependency %s declaration and lock source/bindings differ", name)
		}
	}
	return nil
}

func sameBindings(left, right []manifest.Binding) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
func restoreState(mp string, mb []byte, lp string, lb []byte) error {
	var failures []error
	if len(mb) > 0 {
		if err := lockfile.Atomic(mp, mb, 0o644); err != nil {
			failures = append(failures, fmt.Errorf("restore manifest: %w", err))
		}
	}
	if len(lb) > 0 {
		if err := lockfile.Atomic(lp, lb, 0o644); err != nil {
			failures = append(failures, fmt.Errorf("restore lock: %w", err))
		}
	} else {
		if err := os.Remove(lp); err != nil && !os.IsNotExist(err) {
			failures = append(failures, fmt.Errorf("remove new lock: %w", err))
		}
	}
	return errors.Join(failures...)
}

func withRollback(primary error, failures ...error) error {
	rollbackErr := errors.Join(failures...)
	if rollbackErr == nil {
		return primary
	}
	return errors.Join(primary, fmt.Errorf("rollback failed: %w", rollbackErr))
}
func ensureIgnored(root string) error {
	p := filepath.Join(root, ".gitignore")
	if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf(".gitignore must be a regular file, not a symlink")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	b, e := os.ReadFile(p)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == ".git-a2a/" {
			return nil
		}
	}
	if len(b) > 0 && b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	return lockfile.Atomic(p, append(b, []byte(".git-a2a/\n")...), 0o644)
}
func cleanModulePath(p string) string {
	if p == "" || p == "." {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(p))
}
func defaultPath(p string) string {
	if p == "" {
		return "."
	}
	return p
}
func defaultExportPath(p string) string {
	if p == "" || p == "." {
		return ""
	}
	return p
}
func mustAbs(p string) string {
	v, e := filepath.Abs(p)
	if e != nil {
		return p
	}
	return v
}
func sanitize(v string) string {
	v = strings.ToLower(v)
	var b strings.Builder
	for _, r := range v {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "._-")
}
