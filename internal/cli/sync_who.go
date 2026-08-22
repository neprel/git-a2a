package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	lockfile "github.com/neprel/git-a2a/internal/lock"
	"github.com/neprel/git-a2a/internal/manifest"
	"github.com/neprel/git-a2a/internal/render"
	"github.com/neprel/git-a2a/internal/routing"
)

func (a *App) sync(args []string) int {
	check, brief := false, false
	targets := []string{"AGENTS.md"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--check":
			check = true
		case "--brief":
			brief = true
		case "--target":
			if i+1 >= len(args) {
				fmt.Fprintln(a.Err, "sync: --target needs a file")
				return 2
			}
			i++
			targets = append(targets, args[i])
		default:
			fmt.Fprintf(a.Err, "sync: unknown option %s\n", args[i])
			return 2
		}
	}
	own, err := manifest.Load(filepath.Join(a.root(), "a2amodule.yml"))
	if err != nil {
		fmt.Fprintf(a.Err, "sync: %v\n", err)
		return 2
	}
	l, err := lockfile.Load(a.root())
	if err != nil {
		fmt.Fprintf(a.Err, "sync: %v\n", err)
		return 1
	}
	block, err := render.Build(a.root(), own, l, brief)
	if err != nil {
		fmt.Fprintf(a.Err, "sync: %v\n", err)
		return 1
	}
	seen := map[string]bool{}
	changed := 0
	for _, target := range targets {
		if seen[target] {
			continue
		}
		seen[target] = true
		p := target
		if !filepath.IsAbs(p) {
			p = filepath.Join(a.root(), p)
		}
		different, e := render.Apply(p, block, check)
		if e != nil {
			fmt.Fprintf(a.Err, "sync: %v\n", e)
			return 1
		}
		if different {
			changed++
			fmt.Fprintln(a.Out, target)
		}
	}
	if check && changed > 0 {
		fmt.Fprintf(a.Err, "%d target(s) have stale git-a2a blocks\n", changed)
		return 1
	}
	if changed == 0 {
		fmt.Fprintln(a.Err, "managed blocks are current")
	} else {
		fmt.Fprintf(a.Err, "updated %d managed block(s)\n", changed)
	}
	return 0
}

type whoOutput struct {
	Module  string          `json:"module"`
	Intent  string          `json:"intent"`
	Role    string          `json:"role"`
	Matches []routing.Match `json:"matches"`
}

func (a *App) who(args []string) int {
	intent := "question"
	path := ""
	jsonOut := false
	id := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--intent":
			if i+1 >= len(args) {
				fmt.Fprintln(a.Err, "who: --intent needs a value")
				return 2
			}
			i++
			intent = args[i]
		case "--path":
			if i+1 >= len(args) {
				fmt.Fprintln(a.Err, "who: --path needs a value")
				return 2
			}
			i++
			path = args[i]
		case "--json":
			jsonOut = true
		default:
			if strings.HasPrefix(args[i], "-") {
				fmt.Fprintf(a.Err, "who: unknown option %s\n", args[i])
				return 2
			}
			if id != "" {
				fmt.Fprintln(a.Err, "who: only one module id is allowed")
				return 2
			}
			id = args[i]
		}
	}
	p := filepath.Join(a.root(), "a2amodule.yml")
	if id != "" {
		p = filepath.Join(a.root(), ".git-a2a", "cache", id, "a2amodule.yml")
	}
	m, err := manifest.Load(p)
	if err != nil {
		fmt.Fprintf(a.Err, "who: %v\n", err)
		return 2
	}
	matches, role := routing.Resolve(m, intent, path)
	if len(matches) == 0 {
		owner := "none"
		for _, agent := range m.Agents {
			if agent.Role == "owner" {
				owner = agent.Name
				break
			}
		}
		fmt.Fprintf(a.Err, "nobody is declared for %q on %s; routed role is %s; owner is %s\n", intent, m.Module.ID, role, owner)
		return 2
	}
	if jsonOut {
		b, _ := json.MarshalIndent(whoOutput{Module: m.Module.ID, Intent: intent, Role: role, Matches: matches}, "", "  ")
		fmt.Fprintln(a.Out, string(b))
	} else {
		for _, match := range matches {
			fmt.Fprintf(a.Out, "%s (%s)\n", match.Agent.Name, match.Agent.Role)
			for _, contact := range match.Contacts {
				text := routing.ContactText(contact)
				if contact.Note != "" {
					text += "; " + contact.Note
				}
				fmt.Fprintf(a.Out, "- %s\n", text)
			}
		}
	}
	fmt.Fprintf(a.Err, "%d agent(s) declared for %s\n", len(matches), intent)
	return 0
}
