package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/neprel/git-a2a/internal/a2a"
	"github.com/neprel/git-a2a/internal/cache"
	lockfile "github.com/neprel/git-a2a/internal/lock"
	"github.com/neprel/git-a2a/internal/manifest"
)

func (a *App) card(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(a.Err, "card: expected export, validate, verify, or show")
		return 2
	}
	switch args[0] {
	case "export":
		return a.cardExport(args[1:])
	case "validate":
		return a.cardValidate(args[1:])
	case "verify":
		return a.cardVerify(args[1:])
	case "show":
		return a.cardShow(args[1:])
	default:
		fmt.Fprintf(a.Err, "card: unknown command %s\n", args[0])
		return 2
	}
}

func (a *App) cardVerify(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(a.Err, "card verify: expected one file or URL")
		return 2
	}
	_, raw, err := a2a.Read(args[0], a.root())
	if err != nil {
		fmt.Fprintf(a.Err, "card signature invalid: %v\n", err)
		return 1
	}
	verified, err := a2a.VerifySignatures(raw, a2a.VerifyOptions{CacheRoot: a.root()})
	if err != nil {
		fmt.Fprintf(a.Err, "card signature invalid: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Out, "%s: verified %s signature with key %s\n", args[0], verified.Algorithm, verified.KeyID)
	fmt.Fprintln(a.Err, "card signature verified")
	return 0
}

func (a *App) cardValidate(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(a.Err, "card validate: expected one file or URL")
		return 2
	}
	_, _, err := a2a.Read(args[0], a.root())
	if err != nil {
		fmt.Fprintf(a.Err, "card invalid: %v\n", err)
		return 1
	}
	fmt.Fprintln(a.Out, args[0]+": valid")
	fmt.Fprintln(a.Err, "card valid")
	return 0
}

func (a *App) cardExport(args []string) int {
	agentName, outPath := "", ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--out" {
			if i+1 >= len(args) {
				fmt.Fprintln(a.Err, "card export: --out needs a file")
				return 2
			}
			i++
			outPath = args[i]
		} else if strings.HasPrefix(args[i], "-") {
			fmt.Fprintf(a.Err, "card export: unknown option %s\n", args[i])
			return 2
		} else {
			agentName = args[i]
		}
	}
	if agentName == "" {
		fmt.Fprintln(a.Err, "card export: agent name is required")
		return 2
	}
	m, err := manifest.Load(filepath.Join(a.root(), "a2amodule.yml"))
	if err != nil {
		fmt.Fprintf(a.Err, "card export: %v\n", err)
		return 2
	}
	var agent *manifest.Agent
	for i := range m.Agents {
		if m.Agents[i].Name == agentName {
			agent = &m.Agents[i]
			break
		}
	}
	if agent == nil {
		fmt.Fprintf(a.Err, "card export: unknown agent %s\n", agentName)
		return 2
	}
	var base map[string]any
	if agent.Card != "" {
		snapshot := filepath.Join(cache.Dir(a.root(), m.Module.ID), "cards", a2a.FileName(agent.Name))
		if raw, e := os.ReadFile(snapshot); e == nil {
			base, e = a2a.Parse(raw)
			if e != nil {
				fmt.Fprintf(a.Err, "card export: snapshot: %v\n", e)
				return 1
			}
		} else {
			base, _, e = a2a.Read(agent.Card, a.root())
			if e != nil {
				fmt.Fprintf(a.Err, "card export: no valid snapshot: %v\n", e)
				return 2
			}
		}
	}
	repository := m.Module.Repository
	if repository == "" {
		if out, e := a.runner().Run(a.context(), a.root(), nil, "config", "--get", "remote.origin.url"); e == nil {
			repository = strings.TrimSpace(string(out))
		}
	}
	repository = stripURLUserinfo(repository)
	ref := "HEAD"
	if m.Module.Release != nil && m.Module.Release.Channel != "" {
		ref = m.Module.Release.Channel
	}
	card, err := a2a.Export(base, a2a.Binding{Module: m.Module.ID, Repository: repository, Ref: ref, Agent: *agent, ModuleDescription: m.Module.Description})
	if err != nil {
		fmt.Fprintf(a.Err, "card export: %v\n", err)
		return 2
	}
	b, err := a2a.Marshal(card)
	if err != nil {
		fmt.Fprintf(a.Err, "card export: %v\n", err)
		return 1
	}
	if outPath == "" {
		_, _ = a.Out.Write(b)
	} else {
		p := outPath
		if !filepath.IsAbs(p) {
			p = filepath.Join(a.root(), p)
		}
		if err = lockfile.Atomic(p, b, 0o644); err != nil {
			fmt.Fprintf(a.Err, "card export: %v\n", err)
			return 1
		}
	}
	fmt.Fprintf(a.Err, "exported A2A v1.0 card for %s\n", agentName)
	return 0
}

func stripURLUserinfo(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return raw
	}
	parsed.User = nil
	return parsed.String()
}

func (a *App) cardShow(args []string) int {
	jsonOut := false
	var positional []string
	for _, arg := range args {
		if arg == "--json" {
			jsonOut = true
		} else if strings.HasPrefix(arg, "-") {
			fmt.Fprintf(a.Err, "card show: unknown option %s\n", arg)
			return 2
		} else {
			positional = append(positional, arg)
		}
	}
	if len(positional) > 2 {
		fmt.Fprintln(a.Err, "card show: expected [ID] [AGENT]")
		return 2
	}
	m, err := manifest.Load(filepath.Join(a.root(), "a2amodule.yml"))
	if err != nil {
		fmt.Fprintf(a.Err, "card show: %v\n", err)
		return 2
	}
	id := m.Module.ID
	agentName := ""
	if len(positional) > 0 {
		id = positional[0]
	}
	if len(positional) > 1 {
		agentName = positional[1]
	}
	dir := filepath.Join(cache.Dir(a.root(), id), "cards")
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(a.Err, "card show: no card snapshots for %s\n", id)
		return 2
	}
	shown := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, e := os.ReadFile(filepath.Join(dir, entry.Name()))
		if e != nil {
			continue
		}
		card, e := a2a.Parse(raw)
		if e != nil {
			continue
		}
		name, _ := card["name"].(string)
		if agentName != "" && name != agentName {
			continue
		}
		shown++
		if jsonOut {
			b, _ := json.MarshalIndent(card, "", "  ")
			fmt.Fprintln(a.Out, string(b))
			continue
		}
		version, _ := card["version"].(string)
		interfaces, _ := card["supportedInterfaces"].([]any)
		skills, _ := card["skills"].([]any)
		extensions := 0
		if caps, ok := card["capabilities"].(map[string]any); ok {
			if values, ok := caps["extensions"].([]any); ok {
				extensions = len(values)
			}
		}
		fmt.Fprintf(a.Out, "%s version %s: %d interface(s), %d skill(s), %d extension(s)\n", name, version, len(interfaces), len(skills), extensions)
	}
	if shown == 0 {
		fmt.Fprintln(a.Err, "card show: no matching snapshots")
		return 2
	}
	fmt.Fprintf(a.Err, "showed %d card(s)\n", shown)
	return 0
}
