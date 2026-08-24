package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	lockfile "github.com/neprel/git-a2a/internal/lock"
	"github.com/neprel/git-a2a/internal/manifest"
)

func (a *App) agent(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(a.Err, "agent: expected add, remove, or list")
		return 2
	}
	switch args[0] {
	case "add":
		return a.agentAdd(args[1:])
	case "remove":
		return a.agentRemove(args[1:])
	case "list":
		return a.agentList(args[1:])
	default:
		fmt.Fprintf(a.Err, "agent: unknown action %s\n", args[0])
		return 2
	}
}

func (a *App) agentAdd(args []string) int {
	fs := flag.NewFlagSet("agent add", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	role := fs.String("role", "", "agent role")
	card := fs.String("card", "", "A2A Agent Card URL or file")
	_ = fs.Bool("yes", false, "accept inputs (no-op)")
	var scopes, contacts stringList
	fs.Var(&scopes, "scope", "owned path glob (repeatable)")
	fs.Var(&contacts, "contact", "comma-separated contact fields (repeatable)")
	ordered, orderErr := interspersedArgs(args, map[string]bool{"role": true, "card": true, "scope": true, "contact": true})
	if orderErr != nil {
		fmt.Fprintf(a.Err, "agent add: %v\n", orderErr)
		return 2
	}
	if fs.Parse(ordered) != nil {
		return 2
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" || *role == "" {
		fmt.Fprintln(a.Err, "agent add: NAME and --role are required")
		return 2
	}
	parsedContacts := make([]manifest.Contact, 0, len(contacts))
	for _, raw := range contacts {
		contact, err := parseContact(raw)
		if err != nil {
			fmt.Fprintf(a.Err, "agent add: invalid --contact: %v\n", err)
			return 2
		}
		parsedContacts = append(parsedContacts, contact)
	}
	name := fs.Arg(0)
	added := manifest.Agent{Name: name, Role: *role, Scope: scopes, Card: *card, Contacts: parsedContacts}
	return a.mutateManifest("agent add", func(m *manifest.Manifest) error {
		for _, current := range m.Agents {
			if current.Name == name {
				return fmt.Errorf("agent %s already exists", name)
			}
		}
		m.Agents = append(m.Agents, added)
		return nil
	}, func(original []byte) ([]byte, error) { return manifest.AppendAgent(original, added) }, fmt.Sprintf("added agent %s", name))
}

func (a *App) agentRemove(args []string) int {
	fs := flag.NewFlagSet("agent remove", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	_ = fs.Bool("yes", false, "accept inputs (no-op)")
	ordered, orderErr := interspersedArgs(args, nil)
	if orderErr != nil || fs.Parse(ordered) != nil || fs.NArg() != 1 {
		fmt.Fprintln(a.Err, "agent remove: NAME is required")
		return 2
	}
	name := fs.Arg(0)
	found := false
	code := a.mutateManifest("agent remove", func(m *manifest.Manifest) error {
		kept := m.Agents[:0]
		for _, current := range m.Agents {
			if current.Name == name {
				found = true
				continue
			}
			kept = append(kept, current)
		}
		if !found {
			return errNotFound
		}
		m.Agents = kept
		return nil
	}, func(original []byte) ([]byte, error) { return manifest.RemoveAgent(original, name) }, fmt.Sprintf("removed agent %s", name))
	return code
}

func (a *App) agentList(args []string) int {
	fs := flag.NewFlagSet("agent list", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	jsonOutput := fs.Bool("json", false, "JSON output")
	_ = fs.Bool("yes", false, "accept inputs (no-op)")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return 2
	}
	m, err := manifest.Load(filepath.Join(a.root(), "a2amodule.yml"))
	if err != nil {
		fmt.Fprintf(a.Err, "agent list: %v\n", err)
		return 2
	}
	if len(m.Agents) == 0 {
		fmt.Fprintln(a.Err, "agent list: no agents declared")
		return 2
	}
	agents := append([]manifest.Agent(nil), m.Agents...)
	sort.SliceStable(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	if *jsonOutput {
		body, _ := json.MarshalIndent(agents, "", "  ")
		fmt.Fprintf(a.Out, "%s\n", body)
	} else {
		for _, current := range agents {
			fmt.Fprintf(a.Out, "%s\t%s\t%s\t%d contact(s)\n", current.Name, current.Role, strings.Join(current.Scope, ","), len(current.Contacts))
		}
	}
	fmt.Fprintf(a.Err, "%d agent(s)\n", len(agents))
	return 0
}

func (a *App) export(args []string) int {
	if len(args) == 0 || args[0] != "add" {
		fmt.Fprintln(a.Err, "export: expected add")
		return 2
	}
	fs := flag.NewFlagSet("export add", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	path := fs.String("path", "", "path within module")
	_ = fs.Bool("yes", false, "accept inputs (no-op)")
	ordered, orderErr := interspersedArgs(args[1:], map[string]bool{"path": true})
	if orderErr != nil || fs.Parse(ordered) != nil || fs.NArg() != 2 || fs.Arg(0) == "" || fs.Arg(1) == "" {
		fmt.Fprintln(a.Err, "export add: ECOSYSTEM and NAME are required")
		return 2
	}
	ecosystem, name := fs.Arg(0), fs.Arg(1)
	added := manifest.Export{Ecosystem: ecosystem, Name: name, Path: *path}
	return a.mutateManifest("export add", func(m *manifest.Manifest) error {
		for _, current := range m.Module.Exports {
			if current.Ecosystem == ecosystem && current.Name == name && current.Path == *path {
				return fmt.Errorf("export %s %s already exists", ecosystem, name)
			}
		}
		m.Module.Exports = append(m.Module.Exports, added)
		return nil
	}, func(original []byte) ([]byte, error) { return manifest.AppendExport(original, added) }, fmt.Sprintf("added %s export %s", ecosystem, name))
}

func (a *App) policy(args []string) int {
	if len(args) == 0 || args[0] != "set" {
		fmt.Fprintln(a.Err, "policy: expected set")
		return 2
	}
	fs := flag.NewFlagSet("policy set", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	_ = fs.Bool("yes", false, "accept inputs (no-op)")
	mayRaw := fs.String("may", "", "comma-separated allowed consumer actions")
	mayNotRaw := fs.String("may-not", "", "comma-separated forbidden consumer actions")
	notes := fs.String("notes", "", "policy notes")
	ordered, orderErr := interspersedArgs(args[1:], map[string]bool{"may": true, "may-not": true, "notes": true})
	if orderErr != nil || fs.Parse(ordered) != nil {
		fmt.Fprintln(a.Err, "policy set: expected INTENT=ROLE mappings or --may/--may-not/--notes")
		return 2
	}
	if fs.NArg() == 0 && !flagPresent(args[1:], "may") && !flagPresent(args[1:], "may-not") && !flagPresent(args[1:], "notes") {
		fmt.Fprintln(a.Err, "policy set: expected INTENT=ROLE mappings or --may/--may-not/--notes")
		return 2
	}
	values := make([][2]string, 0, fs.NArg())
	for _, raw := range fs.Args() {
		intent, role, ok := strings.Cut(raw, "=")
		if !ok || intent == "" || role == "" {
			fmt.Fprintf(a.Err, "policy set: invalid mapping %q\n", raw)
			return 2
		}
		values = append(values, [2]string{intent, role})
	}
	var may, mayNot *[]string
	if flagPresent(args[1:], "may") {
		parsed := splitList(*mayRaw)
		may = &parsed
	}
	if flagPresent(args[1:], "may-not") {
		parsed := splitList(*mayNotRaw)
		mayNot = &parsed
	}
	var notesValue *string
	if flagPresent(args[1:], "notes") {
		notesValue = notes
	}
	return a.mutateManifest("policy set", func(m *manifest.Manifest) error {
		if m.Policy == nil {
			m.Policy = &manifest.Policy{}
		}
		if m.Policy.Intents == nil {
			m.Policy.Intents = map[string]string{}
		}
		for _, pair := range values {
			m.Policy.Intents[pair[0]] = pair[1]
		}
		if may != nil || mayNot != nil {
			if m.Policy.Consumers == nil {
				m.Policy.Consumers = &manifest.Consumers{}
			}
			if may != nil {
				m.Policy.Consumers.May = *may
			}
			if mayNot != nil {
				m.Policy.Consumers.MayNot = *mayNot
			}
		}
		if notesValue != nil {
			m.Policy.Notes = *notesValue
		}
		return nil
	}, func(original []byte) ([]byte, error) {
		return manifest.UpdatePolicy(original, values, may, mayNot, notesValue)
	}, fmt.Sprintf("updated policy (%d intent mapping(s))", len(values)))
}

var errNotFound = fmt.Errorf("not found")

func (a *App) mutateManifest(command string, mutate func(*manifest.Manifest) error, edit func([]byte) ([]byte, error), success string) int {
	path := filepath.Join(a.root(), "a2amodule.yml")
	original, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(a.Err, "%s: %v\n", command, err)
		return 2
	}
	m, err := manifest.Parse(original)
	if err != nil {
		fmt.Fprintf(a.Err, "%s: %v\n", command, err)
		return 1
	}
	if err = mutate(m); err != nil {
		fmt.Fprintf(a.Err, "%s: %v\n", command, err)
		if err == errNotFound {
			return 2
		}
		return 1
	}
	if err = m.Validate(); err != nil {
		fmt.Fprintf(a.Err, "%s: %v\n", command, err)
		return 1
	}
	body, err := edit(original)
	if err != nil {
		fmt.Fprintf(a.Err, "%s: %v\n", command, err)
		return 1
	}
	if err = lockfile.Atomic(path, body, 0o644); err != nil {
		fmt.Fprintf(a.Err, "%s: %v\n", command, err)
		return 1
	}
	if err = refreshExistingManagedBlock(a.root()); err != nil {
		_ = lockfile.Atomic(path, original, 0o644)
		_ = refreshExistingManagedBlock(a.root())
		fmt.Fprintf(a.Err, "%s: %v\n", command, err)
		return 1
	}
	fmt.Fprintln(a.Out, success)
	return 0
}

func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func flagPresent(args []string, name string) bool {
	prefix := "--" + name
	for _, arg := range args {
		if arg == prefix || strings.HasPrefix(arg, prefix+"=") {
			return true
		}
	}
	return false
}

func parseContact(raw string) (manifest.Contact, error) {
	values := map[string]string{}
	for _, field := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(field), "=")
		if !ok || key == "" || value == "" {
			return manifest.Contact{}, fmt.Errorf("expected key=value in %q", field)
		}
		if _, duplicate := values[key]; duplicate {
			return manifest.Contact{}, fmt.Errorf("duplicate field %s", key)
		}
		values[key] = value
	}
	list := func(key string) []string {
		if values[key] == "" {
			return nil
		}
		return strings.Split(values[key], "|")
	}
	c := manifest.Contact{
		Intents: list("intents"), Kind: values["kind"], Note: values["note"], URL: values["url"],
		Skill: values["skill"], Address: values["address"], SubjectPrefix: values["subject-prefix"],
		Repo: values["repo"], Labels: list("labels"), Template: values["template"], Project: values["project"],
		Organization: values["organization"], IssueType: values["issue-type"], Channel: values["channel"], Handle: values["handle"], Server: values["server"],
		Method: values["method"], ContentType: values["content-type"], Body: values["body"], Command: list("command"), Args: list("args"), Stdin: values["stdin"],
	}
	known := map[string]bool{"intents": true, "kind": true, "note": true, "url": true, "skill": true, "address": true, "subject-prefix": true, "repo": true, "labels": true, "template": true, "project": true, "organization": true, "issue-type": true, "channel": true, "handle": true, "server": true, "method": true, "content-type": true, "body": true, "command": true, "args": true, "stdin": true}
	for key, value := range values {
		if known[key] {
			continue
		}
		if !strings.HasPrefix(key, "x-") {
			return manifest.Contact{}, fmt.Errorf("unknown field %s", key)
		}
		if c.Extensions == nil {
			c.Extensions = map[string]any{}
		}
		c.Extensions[key] = value
	}
	if len(c.Intents) == 0 || c.Kind == "" {
		return manifest.Contact{}, fmt.Errorf("intents and kind are required")
	}
	probe := &manifest.Manifest{Schema: 1, Module: manifest.Module{ID: "probe"}, Agents: []manifest.Agent{{Name: "probe", Role: "owner", Contacts: []manifest.Contact{c}}}}
	if err := probe.Validate(); err != nil {
		return manifest.Contact{}, err
	}
	return c, nil
}

func exampleManifest(kind, id string) []byte {
	if kind == "lib" {
		return []byte(fmt.Sprintf(`# yaml-language-server: $schema=https://git-a2a.com/schema/a2amodule.v1.json
schema: 1
# Describe the reusable code and the exact native import names consumers should wire.
module:
  id: %s
  name: Example library
  description: A reusable module whose published surface is safe for consumers to read.
  languages: [typescript, python, go]
  surface: surface
  repository: https://github.com/acme/example-lib
  release:
    channel: main
    tags: true
  exports:
    - ecosystem: npm
      name: "@acme/example-lib"
    - ecosystem: pypi
      name: acme-example-lib
    - ecosystem: golang
      name: github.com/acme/example-lib
agents:
  # Bind ownership and contact routes; the Agent Card remains the agent's description.
  - name: acme-lib-owner
    role: owner
    scope: ["**"]
    card: https://agents.acme.example/acme-lib-owner/.well-known/agent-card.json
    contacts:
      - intents: [question, change, bug]
        kind: github-issue
        repo: acme/example-lib
        labels: [from-agent]
policy:
  # Route each request intent to a role and state the consumer boundary.
  intents:
    question: owner
    change: owner
    bug: owner
  consumers:
    may: [read-surface, ask, open-issue, propose-change]
    may-not: [commit, edit-spec, release]
`, id))
	}
	return []byte(fmt.Sprintf(`# yaml-language-server: $schema=https://git-a2a.com/schema/a2amodule.v1.json
schema: 1
# Applications are modules too: they have an identity, owner, and policy even with no exports.
module:
  id: %s
  name: Example consumer application
  description: An application that is also an owned module.
  languages: [typescript]
  repository: https://github.com/acme/consumer-app
agents:
  # This owner is who another module should ask about the application.
  - name: acme-app-owner
    role: owner
    scope: ["**"]
    contacts:
      - intents: [question, change, bug]
        kind: github-issue
        repo: acme/consumer-app
        labels: [from-agent]
policy:
  # Routing is intent -> role -> scoped agent -> matching contact.
  intents:
    question: owner
    change: owner
    bug: owner
  consumers:
    may: [read-surface, ask, open-issue, propose-change]
    may-not: [commit, edit-spec, release]
`, id))
}

// interspersedArgs lets authoring helpers use their documented noun-first form while still
// relying on flag.FlagSet for validation. The standard package otherwise stops at the first
// positional argument.
func interspersedArgs(args []string, valueFlags map[string]bool) ([]string, error) {
	flags := make([]string, 0, len(args))
	positional := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		name := strings.TrimPrefix(arg, "--")
		if key, _, hasValue := strings.Cut(name, "="); hasValue {
			name = key
			continue
		}
		if valueFlags[name] {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--%s needs a value", name)
			}
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, positional...), nil
}
