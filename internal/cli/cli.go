package cli

import (
	"bytes"
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
	"regexp"
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
	"github.com/neprel/git-a2a/internal/render"
	versioninfo "github.com/neprel/git-a2a/internal/version"
)

var Version = versioninfo.Current()
var Commit = "unknown"
var Target = runtime.GOOS + "/" + runtime.GOARCH
var Channel = "go"

type App struct {
	In       io.Reader
	Out, Err io.Writer
	Root     string
	Timeout  time.Duration
	Runner   gitx.Runner
	ctx      context.Context
}

func New(out, errOut io.Writer) *App {
	return &App{Out: out, Err: errOut, Root: ".", Timeout: 120 * time.Second}
}

func (a *App) Run(args []string) int {
	cleanupPreviousUpgrade()
	var timeoutErr error
	args, timeoutErr = a.parseGlobalOptions(args)
	if timeoutErr != nil {
		fmt.Fprintf(a.Err, "git-a2a: %v\n", timeoutErr)
		return 2
	}
	if a.Timeout <= 0 {
		a.Timeout = 120 * time.Second
	}
	commandContext, cancel := context.WithTimeout(context.Background(), a.Timeout)
	a.ctx = commandContext
	defer cancel()
	if len(args) == 0 {
		a.usage()
		return 2
	}
	if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
		a.commandUsage(args[0])
		return 0
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
	case "fetch":
		return a.fetch(args[1:])
	case "sync":
		return a.sync(args[1:])
	case "who":
		return a.who(args[1:])
	case "contact", "ask":
		return a.contact(args[1:])
	case "status":
		return a.status(args[1:])
	case "card":
		return a.card(args[1:])
	case "catalog":
		return a.catalog(args[1:])
	case "fmt":
		return a.format(args[1:])
	case "doctor":
		return a.doctor(args[1:])
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
	fmt.Fprintln(a.Out, "usage: git-a2a <init|validate|add|set|pin|unpin|wire|update|remove|fetch|show|sync|who|contact|status|card|catalog|fmt|doctor|version|upgrade> [options]")
}
func (a *App) commandUsage(command string) {
	usage := map[string]string{
		"init":     "git-a2a init [--id ID] [--description TEXT] [--surface DIR] [--export ECOSYSTEM=NAME]",
		"validate": "git-a2a validate [FILE ...]", "add": "git-a2a add URL [--id ID] [--path DIR] [--track locked|floating] [--wire LIST|--no-wire] [--no-refresh]",
		"set": "git-a2a set ID [--git URL] [--ref REF] [--path DIR] [--track locked|floating] [--id NEW-ID] [--dry-run] [--no-refresh]",
		"pin": "git-a2a pin ID [COMMIT] [--no-refresh]", "unpin": "git-a2a unpin ID --ref REF [--track locked|floating] [--no-refresh]",
		"wire": "git-a2a wire [ID] [--ecosystem NAME] [--no-refresh]", "update": "git-a2a update [ID ...] [--check] [--review|--no-review] [--follow-moves] [--no-refresh]",
		"remove": "git-a2a remove ID [--keep-wiring]", "show": "git-a2a show [ID] [--json] [--surface]",
		"fetch": "git-a2a fetch [ID ...] [--surface] [--json]",
		"sync": "git-a2a sync [--check] [--brief] [--target FILE]", "who": "git-a2a who [ID] [--intent INTENT] [--path FILE] [--json]",
		"contact": "git-a2a contact ID --intent INTENT --message FILE|- [--wait]", "ask": "git-a2a contact ID --intent INTENT --message FILE|- [--wait]",
		"status": "git-a2a status [ID ...] [--offline] [--json] [-v]", "card": "git-a2a card <export|validate|verify|show> [options]",
		"catalog": "git-a2a catalog export [--out FILE]",
		"fmt":     "git-a2a fmt [--check] [PATH...]", "version": "git-a2a version [--check]", "upgrade": "git-a2a upgrade [--to VERSION]",
		"doctor": "git-a2a doctor [--json]",
	}
	if line := usage[command]; line != "" {
		fmt.Fprintln(a.Out, "usage: "+line)
		fmt.Fprintln(a.Out, "global: --timeout DURATION (default 120s)")
		return
	}
	a.usage()
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

func (a *App) context() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) parseGlobalOptions(args []string) ([]string, error) {
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		value := ""
		if args[i] == "--timeout" {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--timeout needs a duration")
			}
			i++
			value = args[i]
		} else if strings.HasPrefix(args[i], "--timeout=") {
			value = strings.TrimPrefix(args[i], "--timeout=")
		} else {
			filtered = append(filtered, args[i])
			continue
		}
		duration, err := time.ParseDuration(value)
		if err != nil || duration <= 0 {
			return nil, fmt.Errorf("invalid --timeout %q", value)
		}
		a.Timeout = duration
	}
	return filtered, nil
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
	_ = fs.Bool("yes", false, "accept defaults (no-op)")
	var exports stringList
	fs.Var(&exports, "export", "ecosystem=name")
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
	if b, err := os.ReadFile(filepath.Join(root, "Cargo.toml")); err == nil {
		if name := tomlNamedSectionValue(string(b), "package", "name"); name != "" {
			out = append(out, manifest.Export{Ecosystem: "cargo", Name: name})
		}
	}
	if b, err := os.ReadFile(filepath.Join(root, "Package.swift")); err == nil {
		if match := regexp.MustCompile(`(?s)Package\s*\(.*?\bname\s*:\s*"([^"]+)"`).FindStringSubmatch(string(b)); len(match) == 2 {
			out = append(out, manifest.Export{Ecosystem: "swift", Name: match[1]})
		}
	}
	if b, err := os.ReadFile(filepath.Join(root, "pubspec.yaml")); err == nil {
		if match := regexp.MustCompile(`(?m)^name:[ \t]*["']?([A-Za-z_][A-Za-z0-9_]*)`).FindStringSubmatch(string(b)); len(match) == 2 {
			out = append(out, manifest.Export{Ecosystem: "pub", Name: match[1]})
		}
	}
	return out
}

func tomlProjectName(s string) string {
	return tomlNamedSectionValue(s, "project", "name")
}

func tomlNamedSectionValue(s, section, key string) string {
	inProject := false
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inProject = line == "["+section+"]"
			continue
		}
		if inProject {
			p := strings.SplitN(line, "=", 2)
			if len(p) == 2 && strings.TrimSpace(p[0]) == key {
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
	var details []string
	for _, p := range paths {
		var err error
		if strings.HasSuffix(p, ".lock") {
			_, err = manifest.LoadLock(p)
		} else {
			_, err = manifest.Load(p)
		}
		if err != nil {
			failed = true
			details = append(details, fmt.Sprintf("%s: %v", p, err))
		} else {
			fmt.Fprintf(a.Out, "%s: valid\n", p)
		}
	}
	if failed {
		fmt.Fprintf(a.Err, "%d file(s): validation failed\n", len(paths))
		for _, detail := range details {
			fmt.Fprintln(a.Err, detail)
		}
		return 1
	}
	fmt.Fprintf(a.Err, "%d file(s): valid\n", len(paths))
	return 0
}

type addOptions struct {
	url, ref, path, id, track string
	wire                      *[]string
	noRefresh                 bool
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
		case "--no-refresh":
			o.noRefresh = true
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
	res, err := f.Fetch(a.context(), o.url, o.ref, o.path, work)
	if err != nil {
		fmt.Fprintf(a.Err, "add: %v\n", err)
		if fetch.IsMissingManifest(err) {
			return 2
		}
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
		o.ref = strings.TrimPrefix(res.Ref, "refs/heads/")
		if o.ref == "" {
			o.ref = "HEAD"
		}
		if depManifest.Module.Release != nil && depManifest.Module.Release.Channel != "" {
			o.ref = depManifest.Module.Release.Channel
			next, e := f.Fetch(a.context(), o.url, o.ref, o.path, work)
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
	if _, err := wireAll(a.context(), preflight, dep, depManifest, locked, false); err != nil {
		fmt.Fprintf(a.Err, "add: wiring preflight: %v; no files changed\n", err)
		return 1
	}
	snapshots := snapshotAdapterFiles(root)
	outcomes, err := wireAll(a.context(), root, dep, depManifest, locked, !o.noRefresh)
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
	if err = refreshExistingManagedBlock(root); err != nil {
		fmt.Fprintf(a.Err, "add: refresh managed block: %v\n", err)
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
		if outcome.Warning != "" {
			fmt.Fprintf(a.Err, "warning: %s\n", outcome.Warning)
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
	path := filepath.Join(root, "a2amodule.yml")
	original, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		original = nil
	} else if err != nil {
		return err
	}
	var b []byte
	if len(original) == 0 {
		b, err = manifest.Marshal(m)
	} else {
		b, err = manifest.UpdateDependencies(original, m.Dependencies)
	}
	if err != nil {
		return err
	}
	return lockfile.Atomic(path, b, 0o644)
}

func refreshExistingManagedBlock(root string) error {
	path := filepath.Join(root, "AGENTS.md")
	if !render.HasManagedBlock(path) {
		return nil
	}
	own, err := manifest.Load(filepath.Join(root, "a2amodule.yml"))
	if err != nil {
		return err
	}
	locked, err := lockfile.Load(root)
	if err != nil {
		return err
	}
	block, err := render.Build(root, own, locked, false)
	if err != nil {
		return err
	}
	_, err = render.Apply(path, block, false)
	return err
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
	noRefresh := false
	review := writerIsTerminal(a.Out)
	var ids []string
	for _, arg := range args {
		if arg == "--check" {
			check = true
		} else if arg == "--follow-moves" {
			followMoves = true
		} else if arg == "--review" {
			review = true
		} else if arg == "--no-review" {
			review = false
		} else if arg == "--no-refresh" {
			noRefresh = true
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
		resolution, e := gitx.ResolveDetailed(a.context(), a.runner(), d.Git, d.Ref)
		if e != nil {
			fmt.Fprintf(a.Err, "update %s: %v\n", d.ID, e)
			return 1
		}
		commit := resolution.Commit
		if resolution.Ambiguous {
			advisories = append(advisories, fmt.Sprintf("%s: ref %s is ambiguous; selected %s", d.ID, d.Ref, resolution.FullRef))
		}
		if entry.Commit == commit && !cacheRepair {
			if cachedManifest, loadErr := manifest.Load(filepath.Join(cache.Dir(root, d.ID), "a2amodule.yml")); loadErr == nil {
				for _, warning := range trustedCardWarnings(cachedManifest, filepath.Join(cache.Dir(root, d.ID), "cards"), root) {
					advisories = append(advisories, fmt.Sprintf("warning: %s card trust: %v", d.ID, warning))
				}
			}
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
		res, e := f.Fetch(a.context(), d.Git, fetchRef, defaultPath(d.Path), work)
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
		oldManifest, _ := os.ReadFile(filepath.Join(cache.Dir(root, d.ID), "a2amodule.yml"))
		if review && !bytes.Equal(oldManifest, res.Manifest) {
			fmt.Fprint(a.Out, textDiff(d.ID+" manifest", oldManifest, res.Manifest))
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
		for _, warning := range trustedCardWarnings(depManifest, filepath.Join(cache.Dir(stagedRoot, d.ID), "cards"), root) {
			advisories = append(advisories, fmt.Sprintf("warning: %s card trust: %v", d.ID, warning))
		}
		surfaceTree := ""
		oldSurface := filepath.Join(cache.Dir(root, d.ID), "surface")
		if depManifest.Module.Surface != "" {
			if info, statErr := os.Stat(oldSurface); statErr == nil && info.IsDir() {
				result, surfaceErr := f.Surface(a.context(), d.Git, res.Commit, defaultPath(d.Path), depManifest.Module.Surface, filepath.Join(cache.Dir(stagedRoot, d.ID), "surface"), filepath.Join(work, "surface-fetch"))
				if surfaceErr != nil {
					_ = os.RemoveAll(work)
					fmt.Fprintf(a.Err, "update %s surface: %v\n", d.ID, surfaceErr)
					return 1
				}
				surfaceTree = result.Tree
				if review {
					oldText := surfaceText(oldSurface)
					newText := surfaceText(filepath.Join(cache.Dir(stagedRoot, d.ID), "surface"))
					if !bytes.Equal(oldText, newText) {
						fmt.Fprint(a.Out, textDiff(d.ID+" surface", oldText, newText))
					}
				}
			}
		}
		entry = manifest.LockedDependency{Git: d.Git, Ref: d.Ref, Path: defaultPath(d.Path), Commit: res.Commit, Manifest: "sha256:" + hex.EncodeToString(sum[:]), Cards: cards, Surface: surfaceTree}
		preflight := filepath.Join(work, "preflight")
		copyAdapterFiles(root, preflight)
		if _, e = wireAll(a.context(), preflight, d, depManifest, entry, false); e != nil {
			_ = os.RemoveAll(work)
			fmt.Fprintf(a.Err, "update %s wiring preflight: %v; no files changed\n", d.ID, e)
			return 1
		}
		snapshots := snapshotAdapterFiles(root)
		outcomes, wireErr := wireAll(a.context(), root, d, depManifest, entry, !noRefresh)
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
			if outcome.Warning != "" {
				advisories = append(advisories, "warning: "+outcome.Warning)
			}
		}
	}
	if found == 0 {
		fmt.Fprintln(a.Err, "update: no dependencies matched")
		return 2
	}
	if check && changed > 0 {
		fmt.Fprintf(a.Err, "%d dependency update(s) available\n", changed)
		for _, advisory := range advisories {
			fmt.Fprintln(a.Err, advisory)
		}
		return 1
	}
	if changed == 0 {
		fmt.Fprintln(a.Err, "dependencies are up to date")
	} else {
		if !check {
			if err := refreshExistingManagedBlock(root); err != nil {
				fmt.Fprintf(a.Err, "update: refresh managed block: %v\n", err)
				return 1
			}
		}
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
		return f.File(a.context(), url, commit, path.Join(defaultPath(modulePath), cardPath))
	}
	return a2a.Snapshot(m, dir, reader)
}

func trustedCardWarnings(m *manifest.Manifest, cardsDir, root string) []error {
	var warnings []error
	for _, agent := range m.Agents {
		if agent.Card == "" || agent.Trust == nil || !agent.Trust.Signatures {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(cardsDir, a2a.FileName(agent.Name)))
		if err == nil {
			_, err = a2a.VerifySignatures(raw, a2a.VerifyOptions{CacheRoot: root})
		}
		if err != nil {
			warnings = append(warnings, fmt.Errorf("%s: %w", agent.Name, err))
		}
	}
	return warnings
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
		if loadErr != nil {
			locked, lockErr := lockfile.Load(root)
			if lockErr != nil {
				fmt.Fprintf(a.Err, "remove: cannot recover wiring metadata for %s: %v\n", id, loadErr)
				return 1
			}
			entry, ok := locked.Dependencies[id]
			if !ok {
				fmt.Fprintf(a.Err, "remove: cannot recover wiring metadata for %s: lock entry missing\n", id)
				return 1
			}
			work, tempErr := os.MkdirTemp("", "git-a2a-remove-")
			if tempErr != nil {
				fmt.Fprintf(a.Err, "remove: %v\n", tempErr)
				return 1
			}
			defer os.RemoveAll(work)
			res, fetchErr := (fetch.Fetcher{Runner: a.runner()}).Fetch(a.context(), entry.Git, entry.Commit, defaultPath(entry.Path), work)
			if fetchErr != nil {
				fmt.Fprintf(a.Err, "remove: recover manifest for unwiring: %v\n", fetchErr)
				return 1
			}
			depManifest, loadErr = manifest.Parse(res.Manifest)
			if loadErr != nil {
				fmt.Fprintf(a.Err, "remove: recovered manifest: %v\n", loadErr)
				return 1
			}
		}
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
				if _, unwireErr := implementation.Unwire(a.context(), root, m.Dependencies[idx], exp); unwireErr != nil {
					fmt.Fprintf(a.Err, "remove: unwire %s: %v\n", exp.Ecosystem, unwireErr)
					return 1
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
	if err := refreshExistingManagedBlock(root); err != nil {
		fmt.Fprintf(a.Err, "remove: refresh managed block: %v\n", err)
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
		l, lockErr := lockfile.Load(root)
		if lockErr != nil {
			fmt.Fprintf(a.Err, "show: lock: %v\n", lockErr)
			return 1
		}
		entry, ok := l.Dependencies[id]
		if d == nil || !ok {
			fmt.Fprintln(a.Err, "show: dependency is not locked")
			return 2
		}
		work, workErr := os.MkdirTemp("", "git-a2a-surface-")
		if workErr != nil {
			fmt.Fprintf(a.Err, "show: surface: %v\n", workErr)
			return 1
		}
		defer os.RemoveAll(work)
		result, e := (fetch.Fetcher{Runner: a.runner()}).Surface(a.context(), d.Git, entry.Commit, defaultPath(d.Path), m.Module.Surface, filepath.Join(cache.Dir(root, id), "surface"), work)
		if e != nil {
			fmt.Fprintf(a.Err, "show: surface: %v\n", e)
			return 1
		}
		entry.Surface = result.Tree
		l.Dependencies[id] = entry
		if e := lockfile.Write(root, l); e != nil {
			fmt.Fprintf(a.Err, "show: surface lock: %v\n", e)
			return 1
		}
		sort.Strings(result.Files)
		for _, name := range result.Files {
			fmt.Fprintln(a.Out, name)
		}
	}
	fmt.Fprintln(a.Err, "module shown")
	return 0
}

func (a *App) format(args []string) int {
	check := false
	paths := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--check" {
			check = true
		} else if strings.HasPrefix(arg, "-") {
			fmt.Fprintf(a.Err, "fmt: unknown argument %q\n", arg)
			return 2
		} else {
			paths = append(paths, arg)
		}
	}
	if len(paths) == 0 {
		paths = append(paths, "a2amodule.yml")
	}
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		p := path
		if !filepath.IsAbs(p) {
			p = filepath.Join(a.root(), p)
		}
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			p = filepath.Join(p, "a2amodule.yml")
		}
		resolved = append(resolved, p)
	}

	type formatResult struct {
		path      string
		formatted []byte
		changed   bool
	}
	results := make([]formatResult, 0, len(resolved))
	for _, p := range resolved {
		original, err := os.ReadFile(p)
		if err != nil {
			if len(paths) == 1 && paths[0] == "a2amodule.yml" {
				fmt.Fprintln(a.Err, "no manifest found")
			} else {
				fmt.Fprintf(a.Err, "fmt: %s: %v\n", p, err)
			}
			return 2
		}
		if _, err = manifest.Parse(original); err != nil {
			fmt.Fprintf(a.Err, "%s: %v\n", p, err)
			return 1
		}
		formatted, err := manifest.Format(original)
		if err != nil {
			fmt.Fprintf(a.Err, "fmt: %s: %v\n", p, err)
			return 1
		}
		results = append(results, formatResult{path: p, formatted: formatted, changed: !bytes.Equal(original, formatted)})
	}
	nonCanonical := 0
	for _, result := range results {
		if result.changed {
			nonCanonical++
		}
	}
	if nonCanonical == 0 {
		if len(results) == 1 {
			fmt.Fprintln(a.Err, "manifest is canonical")
		} else {
			fmt.Fprintf(a.Err, "%d manifests are canonical\n", len(results))
		}
		return 0
	}
	if check {
		fmt.Fprintln(a.Err, "manifest is not canonical")
		return 1
	}
	for _, result := range results {
		if !result.changed {
			continue
		}
		if err := lockfile.Atomic(result.path, result.formatted, 0o644); err != nil {
			fmt.Fprintf(a.Err, "fmt: %s: %v\n", result.path, err)
			return 1
		}
	}
	if len(results) == 1 {
		fmt.Fprintln(a.Err, "manifest formatted")
	} else {
		fmt.Fprintf(a.Err, "%d manifest(s) formatted\n", nonCanonical)
	}
	return 0
}

type wireOutcome struct {
	Ecosystem string
	Reason    string
	Warning   string
	Changed   bool
	Wired     bool
}

func wireAll(ctx context.Context, root string, dep manifest.Dependency, module *manifest.Manifest, locked manifest.LockedDependency, refresh bool) ([]wireOutcome, error) {
	return wireAllUsing(ctx, root, dep, module, locked, refresh, adapters.All())
}

func wireAllUsing(ctx context.Context, root string, dep manifest.Dependency, module *manifest.Manifest, locked manifest.LockedDependency, refresh bool, implementations []adapter.Adapter) ([]wireOutcome, error) {
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
	for _, implementation := range implementations {
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
					if adapter.IsToolUnavailable(err) {
						outcomes[len(outcomes)-1].Warning = err.Error()
						continue
					}
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
