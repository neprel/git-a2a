package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/neprel/git-a2a/internal/cache"
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
	own, err := manifest.LoadDir(a.root())
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
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(a.Err, "sync: %v; run git-a2a fetch\n", err)
		} else {
			fmt.Fprintf(a.Err, "sync: %v\n", err)
		}
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
	Module        string            `json:"module"`
	Intent        string            `json:"intent"`
	Role          string            `json:"role"`
	ContactBudget map[string]string `json:"contactBudget,omitempty"`
	Matches       []whoMatch        `json:"matches"`
}

type whoMatch struct {
	routing.Match
	AcceptsExternal *bool `json:"acceptsExternal,omitempty"`
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
	if id != "" {
		if locked, err := lockfile.Load(a.root()); err == nil {
			if entry, ok := locked.Dependencies[id]; ok && entry.Manifest == "none" {
				fmt.Fprintf(a.Err, "no agents declared: %s is not an a2a module\n", id)
				return 2
			}
		}
	}
	dir := a.root()
	if id != "" {
		dir = cache.Dir(a.root(), id)
	}
	m, err := manifest.LoadDir(dir)
	if err != nil {
		if id != "" && errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(a.Err, "who: %v; run git-a2a fetch\n", err)
		} else {
			fmt.Fprintf(a.Err, "who: %v\n", err)
		}
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
		machineMatches := make([]whoMatch, 0, len(matches))
		for _, match := range matches {
			var acceptsExternal *bool
			if match.Agent.Trust != nil {
				acceptsExternal = match.Agent.Trust.AcceptsExternal
			}
			machineMatches = append(machineMatches, whoMatch{Match: match, AcceptsExternal: acceptsExternal})
		}
		var budget map[string]string
		if m.Policy != nil {
			budget = m.Policy.ContactBudget
		}
		output := any(whoOutput{Module: m.Module.ID, Intent: intent, Role: role, ContactBudget: budget, Matches: machineMatches})
		if id != "" {
			commit := ""
			if locked, lockErr := lockfile.Load(a.root()); lockErr == nil {
				commit = locked.Dependencies[id].Commit
			}
			output = dependencyMachineObject(output, dependencyOrigin(id, commit), "/matches")
		}
		b, _ := json.MarshalIndent(output, "", "  ")
		fmt.Fprintln(a.Out, string(b))
	} else {
		if m.Policy != nil && len(m.Policy.ContactBudget) > 0 {
			keys := make([]string, 0, len(m.Policy.ContactBudget))
			for key := range m.Policy.ContactBudget {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			parts := make([]string, 0, len(keys))
			for _, key := range keys {
				parts = append(parts, key+"="+m.Policy.ContactBudget[key])
			}
			fmt.Fprintf(a.Out, "contact budget (published, not enforced): %s\n", strings.Join(parts, ", "))
		}
		for _, match := range matches {
			external := ""
			if match.Agent.Trust != nil && match.Agent.Trust.AcceptsExternal != nil && !*match.Agent.Trust.AcceptsExternal {
				external = " (external requests not accepted)"
			}
			fmt.Fprintf(a.Out, "%s (%s)%s\n", match.Agent.Name, match.Agent.Role, external)
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
