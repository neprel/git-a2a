package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"reflect"
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
	In            io.Reader
	Out, Err      io.Writer
	Root          string
	Home          string
	Timeout       time.Duration
	Runner        gitx.Runner
	HTTPClient    *http.Client
	ctx           context.Context
	mcpInvocation bool
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
	if len(args) >= 2 && (args[len(args)-1] == "--help" || args[len(args)-1] == "-h") {
		a.commandUsage(strings.Join(args[:len(args)-1], " "))
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
	case "trust":
		return a.trust(args[1:])
	case "usage":
		return a.agentUsage(args[1:])
	case "agent":
		return a.agent(args[1:])
	case "export":
		return a.export(args[1:])
	case "policy":
		return a.policy(args[1:])
	case "explain":
		return a.explain(args[1:])
	case "setup":
		return a.setup(args[1:])
	case "mcp":
		return a.mcp(args[1:])
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
	fmt.Fprintln(a.Out, "usage: git-a2a <command> [options]")
	fmt.Fprintln(a.Out, "\nExamples:")
	fmt.Fprintln(a.Out, "  git-a2a status --offline")
	fmt.Fprintln(a.Out, "  git-a2a who acme-lib --intent change --json")
	fmt.Fprintln(a.Out, "  git-a2a usage --prompt")
	fmt.Fprintln(a.Out, "\nCommands:")
	fmt.Fprintln(a.Out, "  init validate add set pin unpin wire update remove fetch show sync who contact")
	fmt.Fprintln(a.Out, "  status trust card catalog agent export policy explain fmt doctor usage setup mcp version upgrade")
	fmt.Fprintln(a.Out, "\nExit codes:")
	fmt.Fprintln(a.Out, "  0 success or clean check; 1 operational failure or drift; 2 invalid or unresolved input")
}
func (a *App) commandUsage(command string) {
	type help struct{ usage, example string }
	helpByCommand := map[string]help{
		"init":           {"git-a2a init [--id ID] [--description TEXT] [--surface DIR] [--export ECOSYSTEM=NAME] [--example lib|app]", "git-a2a init --id consumer-app --yes"},
		"validate":       {"git-a2a validate [FILE ...] [--json]", "git-a2a validate a2amodule.yml --json"},
		"add":            {"git-a2a add URL [--id ID] [--path DIR] [--track locked|floating] [--wire LIST|--no-wire] [--vendor submodule|copy] [--vendor-path PATH] [--no-refresh] [--insecure-skip-signers]", "git-a2a add https://github.com/acme/lib.git --vendor submodule"},
		"set":            {"git-a2a set ID [--git URL] [--ref REF] [--path DIR] [--track locked|floating] [--id NEW-ID] [--vendor submodule|copy|--no-vendor] [--vendor-path PATH] [--force] [--dry-run] [--no-refresh] [--insecure-skip-signers]", "git-a2a set acme-lib --vendor copy"},
		"pin":            {"git-a2a pin ID [COMMIT] [--no-refresh]", "git-a2a pin acme-lib"},
		"unpin":          {"git-a2a unpin ID --ref REF [--track locked|floating] [--no-refresh]", "git-a2a unpin acme-lib --ref main"},
		"wire":           {"git-a2a wire [ID] [--ecosystem NAME] [--no-refresh]", "git-a2a wire acme-lib --ecosystem npm"},
		"update":         {"git-a2a update [ID ...] [--check] [--review|--no-review] [--follow-moves] [--accept-keys] [--force] [--no-refresh] [--insecure-skip-signers]", "git-a2a update --check"},
		"remove":         {"git-a2a remove ID [--keep-wiring] [--force]", "git-a2a remove acme-lib"},
		"show":           {"git-a2a show [ID] [--json] [--surface]", "git-a2a show acme-lib --surface"},
		"fetch":          {"git-a2a fetch [ID ...] [--surface] [--json] [--insecure-skip-signers]", "git-a2a fetch acme-lib --surface --json"},
		"trust":          {"git-a2a trust show [ID] [--json]", "git-a2a trust show acme-lib --json"},
		"trust show":     {"git-a2a trust show [ID] [--json]", "git-a2a trust show acme-lib --json"},
		"sync":           {"git-a2a sync [--check] [--brief] [--target FILE]", "git-a2a sync --check"},
		"who":            {"git-a2a who [ID] [--intent INTENT] [--path FILE] [--json]", "git-a2a who acme-lib --intent change --json"},
		"contact":        {"git-a2a contact [ID] [--intent INTENT --message FILE|-] [--wait] [--external-ok] [--dry-run] [--list-drivers]", "git-a2a contact acme-lib --list-drivers"},
		"ask":            {"git-a2a contact ID --intent INTENT --message FILE|- [--wait] [--external-ok]", "git-a2a contact acme-lib --intent change --message request.md"},
		"status":         {"git-a2a status [ID ...] [--offline] [--json] [-v]", "git-a2a status --offline --json"},
		"card":           {"git-a2a card <export|validate|verify|show> [options]", "git-a2a card export acme-owner --out agent-card.json"},
		"catalog":        {"git-a2a catalog export [--out FILE]", "git-a2a catalog export --out ai-catalog.json"},
		"fmt":            {"git-a2a fmt [--check] [PATH...]", "git-a2a fmt --check"},
		"version":        {"git-a2a version [--check]", "git-a2a version --check"},
		"upgrade":        {"git-a2a upgrade [--to VERSION]", "git-a2a upgrade --to 1.1.0"},
		"doctor":         {"git-a2a doctor [--json]", "git-a2a doctor --json"},
		"usage":          {"git-a2a usage [--prompt] [--json]", "git-a2a usage --prompt"},
		"agent":          {"git-a2a agent <add|remove|list> [options]", "git-a2a agent list --json"},
		"agent add":      {"git-a2a agent add NAME --role ROLE [--scope GLOB]... [--card URL] [--contact FIELDS]...", "git-a2a agent add acme-owner --role owner --scope '**'"},
		"agent remove":   {"git-a2a agent remove NAME", "git-a2a agent remove acme-old-owner"},
		"agent list":     {"git-a2a agent list [--json]", "git-a2a agent list --json"},
		"export":         {"git-a2a export add ECOSYSTEM NAME [--path PATH]", "git-a2a export add npm @acme/lib"},
		"export add":     {"git-a2a export add ECOSYSTEM NAME [--path PATH]", "git-a2a export add npm @acme/lib"},
		"policy":         {"git-a2a policy set [INTENT=ROLE ...] [--may LIST] [--may-not LIST] [--notes TEXT]", "git-a2a policy set change=owner --may read-surface,ask --may-not commit"},
		"policy set":     {"git-a2a policy set [INTENT=ROLE ...] [--may LIST] [--may-not LIST] [--notes TEXT]", "git-a2a policy set change=owner --may read-surface,ask --may-not commit"},
		"card export":    {"git-a2a card export AGENT [--out FILE]", "git-a2a card export acme-owner --out agent-card.json"},
		"card verify":    {"git-a2a card verify FILE|URL [--jwks URL]... [--key THUMBPRINT]...", "git-a2a card verify agent-card.json --jwks https://agents.acme.example/.well-known/jwks.json"},
		"card validate":  {"git-a2a card validate FILE", "git-a2a card validate agent-card.json"},
		"card show":      {"git-a2a card show FILE|URL", "git-a2a card show agent-card.json"},
		"catalog export": {"git-a2a catalog export [--out FILE]", "git-a2a catalog export --out ai-catalog.json"},
		"explain":        {"git-a2a explain PATH [--json]", "git-a2a explain agents.contacts.kind --json"},
		"setup":          {"git-a2a setup [--check|--dry-run] [--harness LIST|--all]", "git-a2a setup --harness claude-code,codex --dry-run"},
		"mcp":            {"git-a2a mcp [--allow-write] [--roots DIR[,DIR...]]... [--any-root] [--print-roots]", "git-a2a mcp --roots ../acme-lib,../consumer-app"},
	}
	if h, ok := helpByCommand[command]; ok {
		fmt.Fprintln(a.Out, "usage: "+h.usage)
		fmt.Fprintln(a.Out, "\nExamples:")
		fmt.Fprintln(a.Out, "  "+h.example)
		fmt.Fprintln(a.Out, "\nGlobal options:")
		fmt.Fprintln(a.Out, "  --timeout DURATION  Bound the command (default 120s).")
		fmt.Fprintln(a.Out, "  --yes               Confirm automation intent; accepted as a non-interactive no-op.")
		fmt.Fprintln(a.Out, "\nExit codes:")
		fmt.Fprintln(a.Out, "  0  Success, or a check found no drift.")
		fmt.Fprintln(a.Out, "  1  Operational failure, invalid state, or a check found drift.")
		fmt.Fprintln(a.Out, "  2  Invalid invocation, absent input, or unresolved subject.")
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
		} else if args[i] == "--yes" {
			continue
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
	example := fs.String("example", "", "complete commented example: lib or app")
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
	if *example != "" {
		if *example != "lib" && *example != "app" {
			fmt.Fprintf(a.Err, "init: --example must be lib or app\n")
			return 2
		}
		if *desc != "" || *surface != "" || len(exports) > 0 {
			fmt.Fprintln(a.Err, "init: --example cannot be combined with --description, --surface, or --export")
			return 2
		}
		body := exampleManifest(*example, *id)
		if _, err := manifest.Parse(body); err != nil {
			fmt.Fprintf(a.Err, "init: embedded example is invalid: %v\n", err)
			return 1
		}
		if err := lockfile.Atomic(manifestPath, body, 0o644); err != nil {
			fmt.Fprintf(a.Err, "init: %v\n", err)
			return 1
		}
		if err := ensureIgnored(root); err != nil {
			fmt.Fprintf(a.Err, "init: %v\n", err)
			return 1
		}
		fmt.Fprintf(a.Err, "initialized %s example module %s\n", *example, *id)
		return 0
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

type validateResult struct {
	Path  string `json:"path"`
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

func (a *App) validate(args []string) int {
	jsonOutput := false
	paths := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			fmt.Fprintf(a.Err, "validate: unknown option %s\n", arg)
			return 2
		}
		paths = append(paths, arg)
	}
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
	results := make([]validateResult, 0, len(paths))
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
			results = append(results, validateResult{Path: p, Valid: false, Error: err.Error()})
		} else if !jsonOutput {
			fmt.Fprintf(a.Out, "%s: valid\n", p)
			results = append(results, validateResult{Path: p, Valid: true})
		} else {
			results = append(results, validateResult{Path: p, Valid: true})
		}
	}
	if jsonOutput {
		body, _ := json.MarshalIndent(results, "", "  ")
		fmt.Fprintf(a.Out, "%s\n", body)
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
	url, ref, path, id, track, vendorMode, vendorPath string
	wire                                              *[]string
	noRefresh, insecureSkipSigners                    bool
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
		case "--insecure-skip-signers":
			o.insecureSkipSigners = true
		case "--vendor":
			v, e := next()
			if e != nil {
				return o, e
			}
			o.vendorMode = v
		case "--vendor-path":
			v, e := next()
			if e != nil {
				return o, e
			}
			o.vendorPath = v
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
	if o.vendorMode != "" && o.vendorMode != "submodule" && o.vendorMode != "copy" {
		return o, fmt.Errorf("--vendor must be submodule or copy")
	}
	if o.vendorPath != "" && o.vendorMode == "" {
		return o, fmt.Errorf("--vendor-path requires --vendor")
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
	l, err := lockfile.Load(root)
	if err != nil {
		fmt.Fprintf(a.Err, "add: lock: %v\n", err)
		return 1
	}
	predeclared := -1
	for index, d := range own.Dependencies {
		if d.ID == o.id {
			if d.Git == o.url {
				if entry, locked := l.Dependencies[o.id]; locked && entry.Commit != "" {
					fmt.Fprintf(a.Err, "dependency %s already present\n", o.id)
					return 0
				}
				predeclared = index
				break
			}
			fmt.Fprintf(a.Err, "add: dependency %s already uses %s; use git-a2a set %s --git %s to move it\n", o.id, d.Git, o.id, o.url)
			return 1
		}
	}
	dep := manifest.Dependency{ID: o.id, Git: o.url, Ref: o.ref, Path: defaultPath(o.path), Track: o.track, Wire: o.wire}
	if predeclared >= 0 {
		dep = own.Dependencies[predeclared]
	}
	if o.vendorMode != "" {
		dep.Vendor = &manifest.Vendor{Mode: o.vendorMode, Path: o.vendorPath}
	}
	verified, verifyErr := a.verifyCommitTrust(root, dep, res, "", o.insecureSkipSigners, work)
	if verifyErr != nil {
		fmt.Fprintf(a.Err, "add: %v; no files changed\n", verifyErr)
		return 1
	}
	sum := sha256.Sum256(res.Manifest)
	locked := manifest.LockedDependency{Git: o.url, Ref: o.ref, Path: defaultPath(o.path), Commit: res.Commit, Manifest: "sha256:" + hex.EncodeToString(sum[:]), Verified: verified}
	seedVendorLock(own, dep, &locked)
	validated := *own
	validated.Dependencies = append([]manifest.Dependency(nil), own.Dependencies...)
	if predeclared >= 0 {
		validated.Dependencies[predeclared] = dep
	} else {
		validated.Dependencies = append(validated.Dependencies, dep)
	}
	if err = validated.Validate(); err != nil {
		fmt.Fprintf(a.Err, "add: %v; no files changed\n", err)
		return 1
	}
	stagedRoot := filepath.Join(work, "staged-cache")
	if err := cache.Save(stagedRoot, o.id, res.Manifest, res.Commit, res.Method); err != nil {
		fmt.Fprintf(a.Err, "add: stage cache: %v\n", err)
		return 1
	}
	cards, warnings := a.snapshotCardsTo(filepath.Join(cache.Dir(stagedRoot, o.id), "cards"), o.url, o.path, res.Commit, depManifest, f)
	locked.Cards = cards
	cardKeys, trustWarnings := inspectCardTrust(depManifest, filepath.Join(cache.Dir(stagedRoot, o.id), "cards"), root, o.url, res.Commit, dep.Require)
	locked.CardsKeys = cardKeys
	warnings = append(warnings, trustWarnings...)
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
	vendorRollback, err := a.applyVendorTransition(root, own, nil, dep, nil, &locked, false)
	if err != nil {
		restoreAdapterFiles(root, snapshots)
		fmt.Fprintf(a.Err, "add: vendor failed and was rolled back: %v\n", err)
		return 1
	}
	oldManifestBytes, _ := os.ReadFile(filepath.Join(root, "a2amodule.yml"))
	oldLockBytes, _ := os.ReadFile(filepath.Join(root, "a2amodule.lock"))
	if predeclared >= 0 {
		own.Dependencies[predeclared] = dep
	} else {
		own.Dependencies = append(own.Dependencies, dep)
	}
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
		_ = vendorRollback()
		fmt.Fprintf(a.Err, "add: metadata write failed and was rolled back: %v\n", err)
		return 1
	}
	if err = replaceCache(root, o.id, cache.Dir(stagedRoot, o.id), work); err != nil {
		rollbackMetadata()
		restoreAdapterFiles(root, snapshots)
		_ = vendorRollback()
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
	force := false
	acceptKeys := false
	insecureSkipSigners := false
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
		} else if arg == "--force" {
			force = true
		} else if arg == "--accept-keys" {
			acceptKeys = true
		} else if arg == "--insecure-skip-signers" {
			insecureSkipSigners = true
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
		oldEntry := entry
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
			if d.Require != nil && d.Require.Commits == "signed" {
				verifyWork, tempErr := os.MkdirTemp("", "git-a2a-update-signature-")
				if tempErr != nil {
					fmt.Fprintf(a.Err, "update %s: %v\n", d.ID, tempErr)
					return 1
				}
				verified, verifyErr := a.verifyCommitTrust(root, d, fetch.Result{Commit: commit, Ref: resolution.FullRef}, resolution.Kind, insecureSkipSigners, verifyWork)
				_ = os.RemoveAll(verifyWork)
				if verifyErr != nil {
					fmt.Fprintf(a.Err, "update %s: %v; lock unchanged\n", d.ID, verifyErr)
					return 1
				}
				if !check && entry.Verified != verified {
					entry.Verified = verified
					l.Dependencies[d.ID] = entry
					if writeErr := lockfile.Write(root, l); writeErr != nil {
						fmt.Fprintf(a.Err, "update %s signature: %v\n", d.ID, writeErr)
						return 1
					}
					changed++
				}
			}
			if cachedManifest, loadErr := manifest.Load(filepath.Join(cache.Dir(root, d.ID), "a2amodule.yml")); loadErr == nil {
				currentKeys, warnings := inspectCardTrust(cachedManifest, filepath.Join(cache.Dir(root, d.ID), "cards"), root, d.Git, entry.Commit, d.Require)
				for _, warning := range warnings {
					advisories = append(advisories, fmt.Sprintf("warning: %s card trust: %v", d.ID, warning))
				}
				nextKeys, keyWarnings := reconcileCardKeys(entry.CardsKeys, currentKeys, acceptKeys)
				for _, warning := range keyWarnings {
					advisories = append(advisories, fmt.Sprintf("warning: %s card trust: %v", d.ID, warning))
				}
				if acceptKeys && !reflect.DeepEqual(entry.CardsKeys, nextKeys) {
					entry.CardsKeys = nextKeys
					l.Dependencies[d.ID] = entry
					if writeErr := lockfile.Write(root, l); writeErr != nil {
						fmt.Fprintf(a.Err, "update %s accept keys: %v\n", d.ID, writeErr)
						return 1
					}
					changed++
					fmt.Fprintf(a.Out, "%s: accepted card keys\n", d.ID)
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
		verified, verifyErr := a.verifyCommitTrust(root, d, res, resolution.Kind, insecureSkipSigners, work)
		if verifyErr != nil {
			_ = os.RemoveAll(work)
			fmt.Fprintf(a.Err, "update %s: %v; lock unchanged\n", d.ID, verifyErr)
			return 1
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
		if review {
			for agentName := range cards {
				oldCard, _ := os.ReadFile(filepath.Join(cache.Dir(root, d.ID), "cards", a2a.FileName(agentName)))
				newCard, _ := os.ReadFile(filepath.Join(cache.Dir(stagedRoot, d.ID), "cards", a2a.FileName(agentName)))
				if len(oldCard) > 0 && len(newCard) > 0 && !bytes.Equal(oldCard, newCard) {
					fmt.Fprint(a.Out, textDiff(d.ID+" card "+agentName, oldCard, newCard))
				}
			}
		}
		currentKeys, trustWarnings := inspectCardTrust(depManifest, filepath.Join(cache.Dir(stagedRoot, d.ID), "cards"), root, d.Git, res.Commit, d.Require)
		for _, warning := range trustWarnings {
			advisories = append(advisories, fmt.Sprintf("warning: %s card trust: %v", d.ID, warning))
		}
		cardKeys, keyWarnings := reconcileCardKeys(oldEntry.CardsKeys, currentKeys, acceptKeys)
		for _, warning := range keyWarnings {
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
		entry = manifest.LockedDependency{Git: d.Git, Ref: d.Ref, Path: defaultPath(d.Path), Commit: res.Commit, Manifest: "sha256:" + hex.EncodeToString(sum[:]), Cards: cards, CardsKeys: cardKeys, Verified: verified, Surface: surfaceTree}
		seedVendorLock(own, d, &entry)
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
		vendorRollback, vendorErr := a.applyVendorTransition(root, own, &d, d, &oldEntry, &entry, force)
		if vendorErr != nil {
			restoreAdapterFiles(root, snapshots)
			_ = os.RemoveAll(work)
			fmt.Fprintf(a.Err, "update %s vendor failed and was rolled back: %v\n", d.ID, vendorErr)
			return 1
		}
		oldLockBytes, _ := os.ReadFile(filepath.Join(root, "a2amodule.lock"))
		l.Dependencies[d.ID] = entry
		if e = lockfile.Write(root, l); e != nil {
			restoreAdapterFiles(root, snapshots)
			_ = vendorRollback()
			_ = os.RemoveAll(work)
			fmt.Fprintf(a.Err, "update %s lock write failed and was rolled back: %v\n", d.ID, e)
			return 1
		}
		if e = replaceCache(root, d.ID, cache.Dir(stagedRoot, d.ID), work); e != nil {
			_ = lockfile.Atomic(filepath.Join(root, "a2amodule.lock"), oldLockBytes, 0o644)
			restoreAdapterFiles(root, snapshots)
			_ = vendorRollback()
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
	_, warnings := inspectCardTrust(m, cardsDir, root, m.Module.Repository, "", nil)
	return warnings
}

func (a *App) remove(args []string) int {
	keep := false
	force := false
	var id string
	for _, arg := range args {
		if arg == "--keep-wiring" {
			keep = true
		} else if arg == "--force" {
			force = true
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
	l, err := lockfile.Load(root)
	if err != nil {
		fmt.Fprintf(a.Err, "remove: lock: %v\n", err)
		return 1
	}
	entry, hasEntry := l.Dependencies[id]
	if !hasEntry {
		fmt.Fprintf(a.Err, "remove: dependency %s is not locked\n", id)
		return 1
	}
	snapshots := snapshotAdapterFiles(root)
	if !keep {
		depManifest, loadErr := manifest.Load(filepath.Join(cache.Dir(root, id), "a2amodule.yml"))
		if loadErr != nil {
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
					restoreAdapterFiles(root, snapshots)
					fmt.Fprintf(a.Err, "remove: unwire %s: %v\n", exp.Ecosystem, unwireErr)
					return 1
				}
			}
		}
	}
	oldDep := m.Dependencies[idx]
	nextDep := oldDep
	nextDep.Vendor = nil
	nextEntry := entry
	nextEntry.Vendor = nil
	vendorRollback, vendorErr := a.applyVendorTransition(root, m, &oldDep, nextDep, &entry, &nextEntry, force)
	if vendorErr != nil {
		restoreAdapterFiles(root, snapshots)
		fmt.Fprintf(a.Err, "remove: vendor: %v\n", vendorErr)
		return 1
	}
	oldManifestBytes, _ := os.ReadFile(filepath.Join(root, "a2amodule.yml"))
	oldLockBytes, _ := os.ReadFile(filepath.Join(root, "a2amodule.lock"))
	m.Dependencies = append(m.Dependencies[:idx], m.Dependencies[idx+1:]...)
	if err := writeManifest(root, m); err != nil {
		restoreAdapterFiles(root, snapshots)
		_ = vendorRollback()
		fmt.Fprintf(a.Err, "remove: %v\n", err)
		return 1
	}
	delete(l.Dependencies, id)
	if err = lockfile.Write(root, l); err != nil {
		_ = lockfile.Atomic(filepath.Join(root, "a2amodule.yml"), oldManifestBytes, 0o644)
		if len(oldLockBytes) > 0 {
			_ = lockfile.Atomic(filepath.Join(root, "a2amodule.lock"), oldLockBytes, 0o644)
		}
		restoreAdapterFiles(root, snapshots)
		_ = vendorRollback()
		fmt.Fprintf(a.Err, "remove: %v\n", err)
		return 1
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
		if id != "" && errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(a.Err, "show: %v; run git-a2a fetch\n", err)
		} else {
			fmt.Fprintf(a.Err, "show: %v\n", err)
		}
		return 2
	}
	if jsonOut {
		output := any(m)
		if id != "" {
			commit := ""
			if locked, lockErr := lockfile.Load(root); lockErr == nil {
				commit = locked.Dependencies[id].Commit
			}
			output = dependencyMachineObject(m, dependencyOrigin(id, commit), "/module", "/agents", "/policy")
		}
		b, _ := json.MarshalIndent(output, "", "  ")
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
			outcomes = append(outcomes, wireOutcome{Ecosystem: exp.Ecosystem, Changed: change.Changed, Wired: true, Warning: change.Warning})
			if refresh && change.Changed {
				if err := implementation.Refresh(ctx, root, dep, exp, locked); err != nil {
					if adapter.IsToolUnavailable(err) {
						if outcomes[len(outcomes)-1].Warning != "" {
							outcomes[len(outcomes)-1].Warning += "; "
						}
						outcomes[len(outcomes)-1].Warning += err.Error()
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
