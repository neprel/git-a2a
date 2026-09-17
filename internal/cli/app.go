package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/neprel/git-a2a/internal/gitx"
	"github.com/neprel/git-a2a/internal/lifecycle"
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
}

func New(out, errOut io.Writer) *App {
	return &App{Out: out, Err: errOut, Root: ".", Timeout: 120 * time.Second}
}
func (a *App) service() lifecycle.Service { return lifecycle.Service{Root: a.Root, Runner: a.Runner} }

func (a *App) Run(args []string) int {
	if len(args) == 0 {
		a.usage()
		return 2
	}
	if args[0] == "--version" {
		fmt.Fprintf(a.Out, "git-a2a %s (%s, %s, channel=%s)\n", Version, Commit, Target, Channel)
		return 0
	}
	if args[0] == "--help" || args[0] == "-h" {
		a.usage()
		return 0
	}
	if len(args) > 1 && (args[len(args)-1] == "--help" || args[len(args)-1] == "-h") {
		return a.commandHelp(args[0])
	}
	timeout := a.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	switch args[0] {
	case "init":
		return a.init(args[1:])
	case "add":
		return a.add(ctx, args[1:])
	case "pull":
		return a.pull(ctx, args[1:])
	case "remove":
		return a.remove(ctx, args[1:])
	case "list":
		return a.list(args[1:])
	default:
		fmt.Fprintf(a.Err, "git-a2a: unknown command %q\n", args[0])
		a.usage()
		return 2
	}
}

func (a *App) usage() {
	fmt.Fprintln(a.Out, "usage: git-a2a <init|add|pull|remove|list> [options]")
	fmt.Fprintln(a.Out, "\nCommands:\n  init    create a schema 2 component declaration\n  add     add and apply a component dependency\n  pull    apply the latest requested revision for one or all dependencies\n  remove  remove a dependency's owned integration\n  list    inspect dependencies offline\n\nService flags:\n  --help\n  --version")
}
func (a *App) commandHelp(command string) int {
	switch command {
	case "init":
		fmt.Fprintln(a.Out, "usage: git-a2a init [--id ID] [--description TEXT]")
	case "add":
		fmt.Fprintln(a.Out, "usage: git-a2a add SOURCE [--name NAME] [--ref REF] [--path PATH]")
	case "pull":
		fmt.Fprintln(a.Out, "usage: git-a2a pull [NAME]")
	case "remove":
		fmt.Fprintln(a.Out, "usage: git-a2a remove NAME")
	case "list":
		fmt.Fprintln(a.Out, "usage: git-a2a list [NAME] [--json]")
	default:
		fmt.Fprintf(a.Err, "git-a2a: unknown command %q\n", command)
		return 2
	}
	return 0
}

func (a *App) init(args []string) int {
	id, desc := "", ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--id":
			if i+1 >= len(args) {
				return a.usageError("init", "--id needs a value")
			}
			i++
			id = args[i]
		case "--description":
			if i+1 >= len(args) {
				return a.usageError("init", "--description needs a value")
			}
			i++
			desc = args[i]
		default:
			return a.usageError("init", "unknown option "+args[i])
		}
	}
	if err := a.service().Init(id, desc); err != nil {
		fmt.Fprintf(a.Err, "init: %v\n", err)
		return 1
	}
	fmt.Fprintln(a.Err, "initialized a2amodule.yml (add agent.card before publishing this component)")
	return 0
}

func (a *App) add(ctx context.Context, args []string) int {
	source, name, ref, path := "", "", "", ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		next := func() (string, bool) {
			if i+1 >= len(args) {
				return "", false
			}
			i++
			return args[i], true
		}
		switch arg {
		case "--name", "--ref", "--path":
			v, ok := next()
			if !ok {
				return a.usageError("add", arg+" needs a value")
			}
			if arg == "--name" {
				name = v
			} else if arg == "--ref" {
				ref = v
			} else {
				path = v
			}
		default:
			if strings.HasPrefix(arg, "-") {
				return a.usageError("add", "unknown option "+arg)
			}
			if source != "" {
				return a.usageError("add", "exactly one SOURCE is required")
			}
			source = arg
		}
	}
	if source == "" {
		return a.usageError("add", "SOURCE is required")
	}
	r, err := a.service().Add(ctx, source, name, ref, path)
	if err != nil {
		fmt.Fprintf(a.Err, "add: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Err, "added %s at %s\n", r.Name, r.Commit)
	if r.Warning != "" {
		fmt.Fprintf(a.Err, "warning: %s\n", r.Warning)
	}
	return 0
}

func (a *App) pull(ctx context.Context, args []string) int {
	if len(args) > 1 || len(args) == 1 && strings.HasPrefix(args[0], "-") {
		return a.usageError("pull", "expected at most one dependency name")
	}
	name := ""
	if len(args) == 1 {
		name = args[0]
	}
	results, err := a.service().Pull(ctx, name)
	for _, r := range results {
		if r.Error != "" {
			fmt.Fprintf(a.Err, "%s: failed: %s\n", r.Name, r.Error)
		} else if r.Changed {
			fmt.Fprintf(a.Out, "%s: pulled %s\n", r.Name, r.Commit)
		} else {
			fmt.Fprintf(a.Out, "%s: current at %s\n", r.Name, r.Commit)
		}
	}
	if err != nil {
		fmt.Fprintf(a.Err, "pull: %v\n", err)
		return 1
	}
	return 0
}
func (a *App) remove(ctx context.Context, args []string) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return a.usageError("remove", "exactly one dependency name is required")
	}
	r, err := a.service().Remove(ctx, args[0])
	if err != nil {
		fmt.Fprintf(a.Err, "remove: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Err, "removed %s\n", r.Name)
	return 0
}
func (a *App) list(args []string) int {
	name := ""
	jsonOut := false
	for _, arg := range args {
		if arg == "--json" {
			jsonOut = true
		} else if strings.HasPrefix(arg, "-") {
			return a.usageError("list", "unknown option "+arg)
		} else if name != "" {
			return a.usageError("list", "expected at most one dependency name")
		} else {
			name = arg
		}
	}
	items, err := a.service().List(name)
	if err != nil {
		fmt.Fprintf(a.Err, "list: %v\n", err)
		return 1
	}
	if jsonOut {
		b, _ := json.MarshalIndent(items, "", "  ")
		fmt.Fprintf(a.Out, "%s\n", b)
		return 0
	}
	for _, item := range items {
		fmt.Fprintf(a.Out, "%s\t%s\t%s\t%s\n", item.Name, value(item.Component, "unknown"), value(item.Commit, "not-installed"), item.Git)
		if item.Agent != nil {
			fmt.Fprintf(a.Out, "  agent: %s %s\n", item.Agent.Name, item.Agent.Card)
		}
		if item.Surface != "" {
			fmt.Fprintf(a.Out, "  surface: %s\n", item.Surface)
		}
		for _, b := range item.Bindings {
			fmt.Fprintf(a.Out, "  adapter: %s/%s\n", b.Adapter, b.Variant)
		}
		for _, p := range item.Problems {
			fmt.Fprintf(a.Out, "  problem: %s\n", p)
		}
	}
	return 0
}
func (a *App) usageError(command, message string) int {
	fmt.Fprintf(a.Err, "%s: %s\n", command, message)
	return 2
}
func value(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
