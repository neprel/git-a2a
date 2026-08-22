package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/neprel/git-a2a/adapters"
	"github.com/neprel/git-a2a/internal/a2a"
	"github.com/neprel/git-a2a/internal/adapter"
	"github.com/neprel/git-a2a/internal/cache"
	"github.com/neprel/git-a2a/internal/fetch"
	"github.com/neprel/git-a2a/internal/gitx"
	lockfile "github.com/neprel/git-a2a/internal/lock"
	"github.com/neprel/git-a2a/internal/manifest"
	versioninfo "github.com/neprel/git-a2a/internal/version"
)

var Version = versioninfo.Current()
var Commit = "unknown"
var Target = runtime.GOOS + "/" + runtime.GOARCH
var Channel = "go"

type App struct {
	Out, Err io.Writer
	Root     string
	Timeout  time.Duration
	Runner   gitx.Runner
}

func New(out, errOut io.Writer) *App {
	return &App{Out: out, Err: errOut, Root: ".", Timeout: 30 * time.Second}
}

func (a *App) Run(args []string) int {
	if len(args) == 0 {
		a.usage()
		return 2
	}
	if args[0] == "--version" {
		return a.version(nil)
	}
	if args[0] == "version" {
		return a.version(args[1:])
	}
	if args[0] == "upgrade" {
		return a.upgrade(args[1:])
	}
	switch args[0] {
	case "init":
		return a.init(args[1:])
	case "validate":
		return a.validate(args[1:])
	case "add":
		return a.add(args[1:])
	case "update":
		return a.update(args[1:])
	case "set":
		return a.set(args[1:])
	case "pin":
		return a.pin(args[1:])
	case "unpin":
		return a.unpin(args[1:])
	case "wire":
		return a.wire(args[1:])
	case "remove":
		return a.remove(args[1:])
	case "show":
		return a.show(args[1:])
	case "sync":
		return a.sync(args[1:])
	case "who":
		return a.who(args[1:])
	case "status":
		return a.status(args[1:])
	case "card":
		return a.card(args[1:])
	case "fmt":
		return a.format(args[1:])
	case "help", "-h", "--help":
		a.usage()
		return 0
	default:
		fmt.Fprintf(a.Err, "unknown command %q\n", args[0])
		a.usage()
		return 2
	}
}

func (a *App) usage() {
	fmt.Fprintln(a.Out, "usage: git-a2a <init|validate|add|set|pin|unpin|wire|update|remove|show|sync|who|status|card|fmt|version|upgrade> [options]")
}
func (a *App) root() string {
	if a.Root == "" {
		return "."
	}
	return a.Root
}
func (a *App) runner() gitx.Runner {
	if a.Runner != nil {
		return a.Runner
	}
	return gitx.ExecRunner{Timeout: a.Timeout}
}

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func (a *App) init(args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	id := fs.String("id", "", "module id")
	desc := fs.String("description", "", "description")
	surface := fs.String("surface", "", "surface directory")
	var exports stringList
	fs.Var(&exports, "export", "ecosystem=name")
	_ = fs.Bool("yes", false, "non-interactive")
	if fs.Parse(args) != nil {
		return 2
	}
	root := a.root()
	manifestPath := filepath.Join(root, "a2amodule.yml")
	if _, err := os.Stat(manifestPath); err == nil {
		fmt.Fprintln(a.Err, "manifest already exists")
		return 1
	}
	if *id == "" {
		abs, _ := filepath.Abs(root)
		*id = sanitizeID(strings.ToLower(filepath.Base(abs)))
	}
	m := &manifest.Manifest{Schema: 1, Module: manifest.Module{ID: *id, Description: *desc, Surface: *surface}}
	for _, item := range exports {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			fmt.Fprintf(a.Err, "invalid --export %q\n", item)
			return 2
		}
		m.Module.Exports = append(m.Module.Exports, manifest.Export{Ecosystem: parts[0], Name: parts[1]})
	}
	if len(m.Module.Exports) == 0 {
		m.Module.Exports = detectExports(root)
	}
	if err := m.Validate(); err != nil {
		fmt.Fprintf(a.Err, "manifest: %v\n", err)
		return 1
	}
	b, _ := manifest.Marshal(m)
	if err := lockfile.Atomic(manifestPath, b, 0o644); err != nil {
		fmt.Fprintf(a.Err, "init: %v\n", err)
		return 1
	}
	if err := ensureIgnored(root); err != nil {
		fmt.Fprintf(a.Err, "init: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Err, "initialized module %s\n", m.Module.ID)
	return 0
}

func sanitizeID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || strings.ContainsRune("._-", r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.TrimLeft(b.String(), "._-")
}

func detectExports(root string) []manifest.Export {
	var out []manifest.Export
	if b, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil {
		var p struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(b, &p) == nil && p.Name != "" {
			out = append(out, manifest.Export{Ecosystem: "npm", Name: p.Name})
		}
	}
	if b, err := os.ReadFile(filepath.Join(root, "pyproject.toml")); err == nil {
		if name := tomlProjectName(string(b)); name != "" {
			out = append(out, manifest.Export{Ecosystem: "pypi", Name: name})
		}
	}
	if b, err := os.ReadFile(filepath.Join(root, "go.mod")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) == 2 && f[0] == "module" {
				out = append(out, manifest.Export{Ecosystem: "golang", Name: f[1]})
				break
			}
		}
	}
	return out
}

func tomlProjectName(s string) string {
	inProject := false
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inProject = line == "[project]"
			continue
		}
		if inProject && strings.HasPrefix(line, "name") {
			p := strings.SplitN(line, "=", 2)
			if len(p) == 2 {
				return strings.Trim(strings.TrimSpace(p[1]), "\"'")
			}
		}
	}
	return ""
}

func ensureIgnored(root string) error {
	p := filepath.Join(root, ".gitignore")
	b, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == ".git-a2a/" {
			return nil
		}
	}
	if len(b) > 0 && b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	b = append(b, []byte(".git-a2a/\n")...)
	return lockfile.Atomic(p, b, 0o644)
}

func (a *App) validate(paths []string) int {
	if len(paths) == 0 {
		for _, name := range []string{"a2amodule.yml", "a2amodule.lock"} {
			p := filepath.Join(a.root(), name)
			if _, err := os.Stat(p); err == nil {
				paths = append(paths, p)
			}
		}
	}
	if len(paths) == 0 {
		fmt.Fprintln(a.Err, "no manifest or lock found")
		return 2
	}
	failed := false
	for _, p := range paths {
		var err error
		if strings.HasSuffix(p, ".lock") {
			_, err = manifest.LoadLock(p)
		} else {
			_, err = manifest.Load(p)
		}
		if err != nil {
			failed = true
			fmt.Fprintf(a.Err, "%s: %v\n", p, err)
		} else {
			fmt.Fprintf(a.Out, "%s: valid\n", p)
		}
	}
	if failed {
		fmt.Fprintf(a.Err, "%d file(s): validation failed\n", len(paths))
		return 1
	}
	fmt.Fprintf(a.Err, "%d file(s): valid\n", len(paths))
	return 0
}

type addOptions struct {
	url, ref, path, id, track string
	wire                      *[]string
}

func parseAdd(args []string) (addOptions, error) {
	o := addOptions{path: ".", track: "locked"}
	var wires []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		next := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", arg)
			}
			i++
			return args[i], nil
		}
		switch arg {
		case "--path":
			v, e := next()
			if e != nil {
				return o, e
			}
			o.path = v
		case "--id":
			v, e := next()
			if e != nil {
				return o, e
			}
			o.id = v
		case "--track":
			v, e := next()
			if e != nil {
				return o, e
			}
			o.track = v
		case "--wire":
			v, e := next()
			if e != nil {
				return o, e
			}
			wires = strings.Split(v, ",")
			o.wire = &wires
		case "--no-wire":
			wires = []string{}
			o.wire = &wires
		default:
			if strings.HasPrefix(arg, "-") {
				return o, fmt.Errorf("unknown option %s", arg)
			}
			if o.url != "" {
				return o, fmt.Errorf("only one git URL is allowed")
			}
			o.url = arg
		}
	}
	if o.url == "" {
		return o, fmt.Errorf("git URL is required")
	}
	if o.track != "locked" && o.track != "floating" {
		return o, fmt.Errorf("--track must be locked or floating")
	}
	if idx := strings.LastIndex(o.url, "#"); idx > strings.Index(o.url, "://")+2 {
		o.ref = o.url[idx+1:]
		o.url = o.url[:idx]
	}
	return o, nil
}

func (a *App) add(args []string) int {
	o, err := parseAdd(args)
	if err != nil {
		fmt.Fprintf(a.Err, "add: %v\n", err)
		return 2
	}
	root := a.root()
	own, err := manifest.Load(filepath.Join(root, "a2amodule.yml"))
	if err != nil {
		fmt.Fprintf(a.Err, "add: own manifest: %v\n", err)
		return 2
	}
	work, err := os.MkdirTemp("", "git-a2a-add-")
	if err != nil {
		fmt.Fprintf(a.Err, "add: %v\n", err)
		return 1
	}
	defer os.RemoveAll(work)
	f := fetch.Fetcher{Runner: a.runner()}
	res, err := f.Fetch(context.Background(), o.url, o.ref, o.path, work)
	if err != nil {
		fmt.Fprintf(a.Err, "add: %v\n", err)
		return 1
	}
	depManifest, err := manifest.Parse(res.Manifest)
	if err != nil {
		fmt.Fprintf(a.Err, "add: fetched a2amodule.yml: %v\n", err)
		return 1
	}
	if o.id != "" && o.id != depManifest.Module.ID {
		fmt.Fprintf(a.Err, "add: expected module %s, fetched %s\n", o.id, depManifest.Module.ID)
		return 1
	}
	o.id = depManifest.Module.ID
	declaredChannel := ""
	if o.ref == "" {
		o.ref = "HEAD"
		if depManifest.Module.Release != nil && depManifest.Module.Release.Channel != "" {
			o.ref = depManifest.Module.Release.Channel
			next, e := f.Fetch(context.Background(), o.url, o.ref, o.path, work)
			if e != nil {
				fmt.Fprintf(a.Err, "add: release channel %s: %v\n", o.ref, e)
				return 1
			}
			res = next
			depManifest, e = manifest.Parse(res.Manifest)
			if e != nil {
				fmt.Fprintf(a.Err, "add: fetched a2amodule.yml: %v\n", e)
				return 1
			}
			declaredChannel = o.ref
		}
	}
	for _, d := range own.Dependencies {
		if d.ID == o.id {
			if d.Git == o.url {
				fmt.Fprintf(a.Err, "dependency %s already present\n", o.id)
				return 0
			}
			fmt.Fprintf(a.Err, "add: dependency %s already uses %s; use git-a2a set %s --git %s to move it\n", o.id, d.Git, o.id, o.url)
			return 1
		}
	}
	l, err := lockfile.Load(root)
	if err != nil {
		fmt.Fprintf(a.Err, "add: lock: %v\n", err)
		return 1
	}
	dep := manifest.Dependency{ID: o.id, Git: o.url, Ref: o.ref, Path: defaultPath(o.path), Track: o.track, Wire: o.wire}
	sum := sha256.Sum256(res.Manifest)
	locked := manifest.LockedDependency{Git: o.url, Ref: o.ref, Path: defaultPath(o.path), Commit: res.Commit, Manifest: "sha256:" + hex.EncodeToString(sum[:])}
	stagedRoot := filepath.Join(work, "staged-cache")
	if err := cache.Save(stagedRoot, o.id, res.Manifest, res.Commit, res.Method); err != nil {
		fmt.Fprintf(a.Err, "add: stage cache: %v\n", err)
		return 1
	}
	cards, warnings := a.snapshotCardsTo(filepath.Join(cache.Dir(stagedRoot, o.id), "cards"), o.url, o.path, res.Commit, depManifest, f)
	locked.Cards = cards
	preflight := filepath.Join(work, "preflight")
	copyAdapterFiles(root, preflight)
	if _, err := wireAll(context.Background(), preflight, dep, depManifest, locked, false); err != nil {
		fmt.Fprintf(a.Err, "add: wiring preflight: %v; no files changed\n", err)
		return 1
	}
	snapshots := snapshotAdapterFiles(root)
	outcomes, err := wireAll(context.Background(), root, dep, depManifest, locked, true)
	if err != nil {
		restoreAdapterFiles(root, snapshots)
		fmt.Fprintf(a.Err, "add: wiring failed and was rolled back: %v\n", err)
		return 1
	}
	oldManifestBytes, _ := os.ReadFile(filepath.Join(root, "a2amodule.yml"))
	oldLockBytes, _ := os.ReadFile(filepath.Join(root, "a2amodule.lock"))
	own.Dependencies = append(own.Dependencies, dep)
	l.Dependencies[o.id] = locked
	if err = writeManifest(root, own); err == nil {
		err = lockfile.Write(root, l)
	}
	rollbackMetadata := func() {
		_ = lockfile.Atomic(filepath.Join(root, "a2amodule.yml"), oldManifestBytes, 0o644)
		if len(oldLockBytes) > 0 {
			_ = lockfile.Atomic(filepath.Join(root, "a2amodule.lock"), oldLockBytes, 0o644)
		} else {
			_ = os.Remove(filepath.Join(root, "a2amodule.lock"))
		}
	}
	if err != nil {
		rollbackMetadata()
		restoreAdapterFiles(root, snapshots)
		fmt.Fprintf(a.Err, "add: metadata write failed and was rolled back: %v\n", err)
		return 1
	}
	if err = replaceCache(root, o.id, cache.Dir(stagedRoot, o.id), work); err != nil {
		rollbackMetadata()
		restoreAdapterFiles(root, snapshots)
		fmt.Fprintf(a.Err, "add: cache replacement failed and was rolled back: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Err, "added %s at %s\n", o.id, res.Commit)
	if declaredChannel != "" {
		fmt.Fprintf(a.Err, "using declared release channel %s\n", declaredChannel)
	}
	for _, warning := range warnings {
		fmt.Fprintf(a.Err, "warning: card snapshot: %v\n", warning)
	}
	for _, outcome := range outcomes {
		if !outcome.Wired {
			fmt.Fprintf(a.Err, "%s: not wired: %s\n", outcome.Ecosystem, outcome.Reason)
		}
	}
	return 0
}

func defaultPath(p string) string {
	if p == "" {
		return "."
	}
	return p
}
func writeManifest(root string, m *manifest.Manifest) error {
	b, err := manifest.Marshal(m)
	if err != nil {
		return err
	}
	return lockfile.Atomic(filepath.Join(root, "a2amodule.yml"), b, 0o644)
}
func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	if s == "" {
		return "unlocked"
	}
	return s
}

func (a *App) update(args []string) int {
	check := false
	followMoves := false
	var ids []string
	for _, arg := range args {
		if arg == "--check" {
			check = true
		} else if arg == "--follow-moves" {
			followMoves = true
		} else if strings.HasPrefix(arg, "-") {
			fmt.Fprintf(a.Err, "update: unknown option %s\n", arg)
			return 2
		} else {
			ids = append(ids, arg)
		}
	}
	root := a.root()
	own, err := manifest.Load(filepath.Join(root, "a2amodule.yml"))
	if err != nil {
		fmt.Fprintf(a.Err, "update: own manifest: %v\n", err)
		return 2
	}
	l, err := lockfile.Load(root)
	if err != nil {
		fmt.Fprintf(a.Err, "update: lock: %v\n", err)
		return 1
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	changed, found := 0, 0
	var advisories []string
	f := fetch.Fetcher{Runner: a.runner()}
	for _, d := range own.Dependencies {
		if len(wanted) > 0 && !wanted[d.ID] {
			continue
		}
		found++
		entry := l.Dependencies[d.ID]
		cacheRepair := cacheNeedsRepair(root, d.ID, entry.Manifest)
		resolution, e := gitx.ResolveDetailed(context.Background(), a.runner(), d.Git, d.Ref)
		if e != nil {
			fmt.Fprintf(a.Err, "update %s: %v\n", d.ID, e)
			return 1
		}
		commit := resolution.Commit
		if resolution.Ambiguous {
			advisories = append(advisories, fmt.Sprintf("%s: ref %s is ambiguous; selected %s", d.ID, d.Ref, resolution.FullRef))
		}
		if entry.Commit == commit && !cacheRepair {
			continue
		}
		changed++
		if cacheRepair && entry.Commit == commit {
			fmt.Fprintf(a.Out, "%s: restore cache at %s\n", d.ID, short(commit))
		} else {
			fmt.Fprintf(a.Out, "%s: %s -> %s\n", d.ID, short(entry.Commit), short(commit))
		}
		if check {
			continue
		}
		work, tempErr := os.MkdirTemp("", "git-a2a-update-")
		if tempErr != nil {
			fmt.Fprintf(a.Err, "update %s: %v\n", d.ID, tempErr)
			return 1
		}
		fetchRef := d.Ref
		if cacheRepair && entry.Commit == commit {
			fetchRef = entry.Commit
		}
		res, e := f.Fetch(context.Background(), d.Git, fetchRef, defaultPath(d.Path), work)
		if e != nil {
			_ = os.RemoveAll(work)
			fmt.Fprintf(a.Err, "update %s: %v\n", d.ID, e)
			return 1
		}
		depManifest, e := manifest.Parse(res.Manifest)
		if e != nil {
			_ = os.RemoveAll(work)
			fmt.Fprintf(a.Err, "update %s: %v\n", d.ID, e)
			return 1
		}
		if depManifest.Module.MovedTo != nil {
			move := depManifest.Module.MovedTo
			if !followMoves {
				_ = os.RemoveAll(work)
				fmt.Fprintf(a.Err, "%s moved to %s; run update --follow-moves or git-a2a set %s --git %s\n", d.ID, move.Git, d.ID, move.Git)
				return 1
			}
			opts := setOptions{id: d.ID, git: &move.Git}
			if move.Path != "" {
				opts.path = &move.Path
			}
			_ = os.RemoveAll(work)
			if code := a.applySet(opts); code != 0 {
				return code
			}
			l, e = lockfile.Load(root)
			if e != nil {
				fmt.Fprintf(a.Err, "update: lock after move: %v\n", e)
				return 1
			}
			continue
		}
		if resolution.Kind == "tag" && entry.Commit != "" && entry.Commit != res.Commit {
			advisories = append(advisories, fmt.Sprintf("tag %s moved from %s to %s", d.Ref, short(entry.Commit), short(res.Commit)))
		}
		sum := sha256.Sum256(res.Manifest)
		stagedRoot := filepath.Join(work, "staged-cache")
		if e = cache.Save(stagedRoot, d.ID, res.Manifest, res.Commit, res.Method); e != nil {
			_ = os.RemoveAll(work)
			fmt.Fprintf(a.Err, "update %s: stage cache: %v\n", d.ID, e)
			return 1
		}
		cards, warnings := a.snapshotCardsTo(filepath.Join(cache.Dir(stagedRoot, d.ID), "cards"), d.Git, d.Path, res.Commit, depManifest, f)
		for _, warning := range warnings {
			advisories = append(advisories, fmt.Sprintf("warning: %s card snapshot: %v", d.ID, warning))
		}
		entry = manifest.LockedDependency{Git: d.Git, Ref: d.Ref, Path: defaultPath(d.Path), Commit: res.Commit, Manifest: "sha256:" + hex.EncodeToString(sum[:]), Cards: cards, Surface: entry.Surface}
		preflight := filepath.Join(work, "preflight")
		copyAdapterFiles(root, preflight)
		if _, e = wireAll(context.Background(), preflight, d, depManifest, entry, false); e != nil {
			_ = os.RemoveAll(work)
			fmt.Fprintf(a.Err, "update %s wiring preflight: %v; no files changed\n", d.ID, e)
			return 1
		}
		snapshots := snapshotAdapterFiles(root)
		outcomes, wireErr := wireAll(context.Background(), root, d, depManifest, entry, true)
		if wireErr != nil {
			restoreAdapterFiles(root, snapshots)
			_ = os.RemoveAll(work)
			fmt.Fprintf(a.Err, "update %s wiring failed and was rolled back: %v\n", d.ID, wireErr)
			return 1
		}
		oldLockBytes, _ := os.ReadFile(filepath.Join(root, "a2amodule.lock"))
		l.Dependencies[d.ID] = entry
		if e = lockfile.Write(root, l); e != nil {
			restoreAdapterFiles(root, snapshots)
			_ = os.RemoveAll(work)
			fmt.Fprintf(a.Err, "update %s lock write failed and was rolled back: %v\n", d.ID, e)
			return 1
		}
		if e = replaceCache(root, d.ID, cache.Dir(stagedRoot, d.ID), work); e != nil {
			_ = lockfile.Atomic(filepath.Join(root, "a2amodule.lock"), oldLockBytes, 0o644)
			restoreAdapterFiles(root, snapshots)
			_ = os.RemoveAll(work)
			fmt.Fprintf(a.Err, "update %s cache replacement failed and was rolled back: %v\n", d.ID, e)
			return 1
		}
		_ = os.RemoveAll(work)
		for _, outcome := range outcomes {
			if !outcome.Wired {
				advisories = append(advisories, fmt.Sprintf("%s: %s not wired: %s", d.ID, outcome.Ecosystem, outcome.Reason))
			}
		}
	}
	if found == 0 {
		fmt.Fprintln(a.Err, "update: no dependencies matched")
		return 2
	}
	if check && changed > 0 {
		fmt.Fprintf(a.Err, "%d dependency update(s) available\n", changed)
		return 1
	}
	if changed == 0 {
		fmt.Fprintln(a.Err, "dependencies are up to date")
	} else {
		fmt.Fprintf(a.Err, "updated %d dependency(s)\n", changed)
	}
	for _, advisory := range advisories {
		fmt.Fprintln(a.Err, advisory)
	}
	return 0
}

func cacheNeedsRepair(root, id, expectedHash string) bool {
	b, err := os.ReadFile(filepath.Join(cache.Dir(root, id), "a2amodule.yml"))
	if err != nil {
		return true
	}
	sum := sha256.Sum256(b)
	return "sha256:"+hex.EncodeToString(sum[:]) != expectedHash
}

func (a *App) snapshotCards(root, id, url, modulePath, commit string, m *manifest.Manifest, f fetch.Fetcher) (map[string]string, []error) {
	return a.snapshotCardsTo(filepath.Join(cache.Dir(root, id), "cards"), url, modulePath, commit, m, f)
}

func (a *App) snapshotCardsTo(dir, url, modulePath, commit string, m *manifest.Manifest, f fetch.Fetcher) (map[string]string, []error) {
	reader := func(cardPath string) ([]byte, error) {
		return f.File(context.Background(), url, commit, path.Join(defaultPath(modulePath), cardPath))
	}
	return a2a.Snapshot(m, dir, reader)
}

func (a *App) remove(args []string) int {
	keep := false
	var id string
	for _, arg := range args {
		if arg == "--keep-wiring" {
			keep = true
		} else if strings.HasPrefix(arg, "-") {
			fmt.Fprintf(a.Err, "remove: unknown option %s\n", arg)
			return 2
		} else {
			id = arg
		}
	}
	if id == "" {
		fmt.Fprintln(a.Err, "remove: dependency id is required")
		return 2
	}
	root := a.root()
	m, err := manifest.Load(filepath.Join(root, "a2amodule.yml"))
	if err != nil {
		fmt.Fprintf(a.Err, "remove: %v\n", err)
		return 2
	}
	idx := -1
	for i, d := range m.Dependencies {
		if d.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		fmt.Fprintf(a.Err, "remove: unknown dependency %s\n", id)
		return 2
	}
	if !keep {
		depManifest, loadErr := manifest.Load(filepath.Join(cache.Dir(root, id), "a2amodule.yml"))
		if loadErr == nil {
			for _, implementation := range adapters.All() {
				for _, exp := range depManifest.Module.Exports {
					if exp.Ecosystem != implementation.Ecosystem() {
						continue
					}
					ok, _, detectErr := implementation.Detect(root)
					if detectErr != nil {
						fmt.Fprintf(a.Err, "remove: %v\n", detectErr)
						return 1
					}
					if !ok {
						continue
					}
					if _, unwireErr := implementation.Unwire(context.Background(), root, m.Dependencies[idx], exp); unwireErr != nil {
						fmt.Fprintf(a.Err, "remove: unwire %s: %v\n", exp.Ecosystem, unwireErr)
						return 1
					}
				}
			}
		}
	}
	m.Dependencies = append(m.Dependencies[:idx], m.Dependencies[idx+1:]...)
	if err := writeManifest(root, m); err != nil {
		fmt.Fprintf(a.Err, "remove: %v\n", err)
		return 1
	}
	l, err := lockfile.Load(root)
	if err == nil {
		delete(l.Dependencies, id)
		if err = lockfile.Write(root, l); err != nil {
			fmt.Fprintf(a.Err, "remove: %v\n", err)
			return 1
		}
	}
	if err := os.RemoveAll(cache.Dir(root, id)); err != nil {
		fmt.Fprintf(a.Err, "remove: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Err, "removed %s (cache deleted; it can be recreated by add)\n", id)
	return 0
}

func (a *App) show(args []string) int {
	jsonOut, surface := false, false
	var id string
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOut = true
		case "--surface":
			surface = true
		default:
			if strings.HasPrefix(arg, "-") {
				fmt.Fprintf(a.Err, "show: unknown option %s\n", arg)
				return 2
			}
			id = arg
		}
	}
	root := a.root()
	p := filepath.Join(root, "a2amodule.yml")
	if id != "" {
		p = filepath.Join(cache.Dir(root, id), "a2amodule.yml")
	}
	m, err := manifest.Load(p)
	if err != nil {
		fmt.Fprintf(a.Err, "show: %v\n", err)
		return 2
	}
	if jsonOut {
		b, _ := json.MarshalIndent(m, "", "  ")
		fmt.Fprintln(a.Out, string(b))
	} else {
		fmt.Fprintf(a.Out, "%s\n%s\n", m.Module.ID, m.Module.Description)
		for _, agent := range m.Agents {
			fmt.Fprintf(a.Out, "agent: %s (%s)\n", agent.Name, agent.Role)
		}
	}
	if surface {
		if id == "" || m.Module.Surface == "" {
			fmt.Fprintln(a.Err, "show: no dependency surface declared")
			return 2
		}
		own, _ := manifest.Load(filepath.Join(root, "a2amodule.yml"))
		var d *manifest.Dependency
		for i := range own.Dependencies {
			if own.Dependencies[i].ID == id {
				d = &own.Dependencies[i]
			}
		}
		l, _ := lockfile.Load(root)
		entry, ok := l.Dependencies[id]
		if d == nil || !ok {
			fmt.Fprintln(a.Err, "show: dependency is not locked")
			return 2
		}
		names, e := (fetch.Fetcher{Runner: a.runner()}).Surface(context.Background(), d.Git, entry.Commit, defaultPath(d.Path), m.Module.Surface, filepath.Join(cache.Dir(root, id), "surface"))
		if e != nil {
			fmt.Fprintf(a.Err, "show: surface: %v\n", e)
			return 1
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintln(a.Out, name)
		}
	}
	fmt.Fprintln(a.Err, "module shown")
	return 0
}

func (a *App) format(args []string) int {
	check := false
	for _, arg := range args {
		if arg == "--check" {
			check = true
		} else {
			fmt.Fprintf(a.Err, "fmt: unknown argument %q\n", arg)
			return 2
		}
	}
	p := filepath.Join(a.root(), "a2amodule.yml")
	original, err := os.ReadFile(p)
	if err != nil {
		fmt.Fprintln(a.Err, "no manifest found")
		return 2
	}
	m, err := manifest.Parse(original)
	if err != nil {
		fmt.Fprintf(a.Err, "%s: %v\n", p, err)
		return 1
	}
	formatted, err := manifest.Marshal(m)
	if err != nil {
		fmt.Fprintf(a.Err, "fmt: %v\n", err)
		return 1
	}
	if string(original) == string(formatted) {
		fmt.Fprintln(a.Err, "manifest is canonical")
		return 0
	}
	if check {
		fmt.Fprintln(a.Err, "manifest is not canonical")
		return 1
	}
	if err := lockfile.Atomic(p, formatted, 0o644); err != nil {
		fmt.Fprintf(a.Err, "fmt: %v\n", err)
		return 1
	}
	fmt.Fprintln(a.Err, "manifest formatted")
	return 0
}

type wireOutcome struct {
	Ecosystem string
	Reason    string
	Changed   bool
	Wired     bool
}

func wireAll(ctx context.Context, root string, dep manifest.Dependency, module *manifest.Manifest, locked manifest.LockedDependency, refresh bool) ([]wireOutcome, error) {
	wanted := map[string]bool{}
	if dep.Wire != nil {
		for _, ecosystem := range *dep.Wire {
			wanted[ecosystem] = true
		}
		if len(*dep.Wire) == 0 {
			return nil, nil
		}
	}
	wired := map[string]bool{}
	var outcomes []wireOutcome
	for _, implementation := range adapters.All() {
		if dep.Wire != nil && !wanted[implementation.Ecosystem()] {
			continue
		}
		ok, _, err := implementation.Detect(root)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		for _, exp := range module.Module.Exports {
			if exp.Ecosystem != implementation.Ecosystem() {
				continue
			}
			change, err := implementation.Wire(ctx, root, dep, exp, locked)
			if err != nil {
				if dep.Wire == nil && adapter.IsNotWirable(err) {
					outcomes = append(outcomes, wireOutcome{Ecosystem: exp.Ecosystem, Reason: adapter.NotWirableReason(err)})
					continue
				}
				return nil, err
			}
			wired[exp.Ecosystem] = true
			outcomes = append(outcomes, wireOutcome{Ecosystem: exp.Ecosystem, Changed: change.Changed, Wired: true})
			if refresh && change.Changed {
				if err := implementation.Refresh(ctx, root, dep, exp, locked); err != nil {
					return nil, err
				}
			}
		}
	}
	if dep.Wire != nil {
		for ecosystem := range wanted {
			if !wired[ecosystem] {
				return nil, fmt.Errorf("ecosystem %s was requested but is not wirable in this repository", ecosystem)
			}
		}
	}
	return outcomes, nil
}
