package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	contactcore "github.com/neprel/git-a2a/internal/contact"
	contacta2a "github.com/neprel/git-a2a/internal/contact/a2a"
	contactgithub "github.com/neprel/git-a2a/internal/contact/githubissue"
	contactinstruction "github.com/neprel/git-a2a/internal/contact/instruction"
	"github.com/neprel/git-a2a/internal/manifest"
	"github.com/neprel/git-a2a/internal/routing"
)

func (a *App) contact(args []string) int {
	id, intent, messagePath := "", "question", ""
	wait, externalOK := false, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--intent":
			if i+1 >= len(args) {
				fmt.Fprintln(a.Err, "contact: --intent needs a value")
				return 2
			}
			i++
			intent = args[i]
		case "--message":
			if i+1 >= len(args) {
				fmt.Fprintln(a.Err, "contact: --message needs a file or -")
				return 2
			}
			i++
			messagePath = args[i]
		case "--wait":
			wait = true
		case "--external-ok":
			externalOK = true
		default:
			if strings.HasPrefix(args[i], "-") {
				fmt.Fprintf(a.Err, "contact: unknown option %s\n", args[i])
				return 2
			}
			if id != "" {
				fmt.Fprintln(a.Err, "contact: only one module id is allowed")
				return 2
			}
			id = args[i]
		}
	}
	if id == "" || messagePath == "" {
		fmt.Fprintln(a.Err, "contact: module id and --message FILE|- are required")
		return 2
	}
	message, err := a.readContactMessage(messagePath)
	if err != nil {
		fmt.Fprintf(a.Err, "contact: message: %v\n", err)
		return 2
	}
	if strings.TrimSpace(message) == "" {
		fmt.Fprintln(a.Err, "contact: message is empty")
		return 2
	}
	m, err := manifest.Load(filepath.Join(a.root(), ".git-a2a", "cache", id, "a2amodule.yml"))
	if err != nil {
		fmt.Fprintf(a.Err, "contact: %v\n", err)
		return 2
	}
	matches, role := routing.Resolve(m, intent, "")
	var consumer *manifest.Manifest
	if matchesDeclineExternal(matches) {
		consumer, err = manifest.Load(filepath.Join(a.root(), "a2amodule.yml"))
		if err != nil {
			fmt.Fprintf(a.Err, "contact: own manifest: %v\n", err)
			return 2
		}
	}
	if externalRequestRefused(consumer, m, matches) && !externalOK {
		fmt.Fprintln(a.Err, "contact: owner does not accept external requests; pass --external-ok only after human approval")
		return 2
	}
	drivers := []contactcore.Driver{contacta2a.Driver{}, contactgithub.Driver{}}
	for _, kind := range []string{"url", "email", "mattermost", "slack", "discord", "telegram", "teams"} {
		drivers = append(drivers, contactinstruction.Driver{ContactKind: kind})
	}
	for _, match := range matches {
		for _, declared := range match.Contacts {
			for _, driver := range drivers {
				if driver.Kind() != declared.Kind {
					continue
				}
				record, deliveryErr := driver.Deliver(a.context(), contactcore.Request{
					Agent: match.Agent.Name, Contact: declared, Message: message, Wait: wait,
				})
				if deliveryErr != nil {
					fmt.Fprintf(a.Err, "contact: %s via %s: %v\n", match.Agent.Name, declared.Kind, deliveryErr)
					return 1
				}
				fmt.Fprintln(a.Out, record.String())
				if externalOK && externalRequestRefused(consumer, m, matches) {
					fmt.Fprintln(a.Err, "external request override recorded in delivery output")
				}
				return 0
			}
		}
	}
	if len(matches) == 0 {
		fmt.Fprintf(a.Err, "contact: nobody is declared for %q on %s; routed role is %s\n", intent, id, role)
	} else {
		fmt.Fprintf(a.Err, "contact: no supported delivery kind is declared for %q on %s\n", intent, id)
	}
	return 2
}

func externalRequestRefused(consumer, owner *manifest.Manifest, matches []routing.Match) bool {
	if !matchesDeclineExternal(matches) {
		return false
	}
	consumerOrganisations := moduleOrganisations(consumer)
	ownerOrganisations := moduleOrganisations(owner)
	for _, left := range consumerOrganisations {
		for _, right := range ownerOrganisations {
			if strings.EqualFold(left, right) {
				return false
			}
		}
	}
	return true
}

func matchesDeclineExternal(matches []routing.Match) bool {
	for _, match := range matches {
		if match.Agent.Trust != nil && match.Agent.Trust.AcceptsExternal != nil && !*match.Agent.Trust.AcceptsExternal {
			return true
		}
	}
	return false
}

func moduleOrganisations(module *manifest.Manifest) []string {
	if module.Settings != nil && len(module.Settings.Organisation) > 0 {
		result := make([]string, 0, len(module.Settings.Organisation))
		for _, value := range module.Settings.Organisation {
			result = append(result, strings.TrimPrefix(gitOrganisation(value), "//"))
		}
		return result
	}
	if organisation := gitOrganisation(module.Module.Repository); organisation != "" {
		return []string{organisation}
	}
	return nil
}

func gitOrganisation(repository string) string {
	normalized := strings.TrimPrefix(strings.TrimSpace(repository), "git+")
	if strings.HasPrefix(normalized, "git@") {
		normalized = strings.Replace(strings.TrimPrefix(normalized, "git@"), ":", "/", 1)
	} else if marker := strings.Index(normalized, "://"); marker >= 0 {
		normalized = normalized[marker+3:]
		normalized = strings.TrimPrefix(normalized, "git@")
	}
	normalized = strings.Trim(normalized, "/")
	parts := strings.Split(normalized, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return strings.ToLower(normalized)
	}
	return strings.ToLower(parts[0] + "/" + parts[1])
}

func (a *App) readContactMessage(path string) (string, error) {
	if path == "-" {
		reader := a.In
		if reader == nil {
			reader = os.Stdin
		}
		body, err := io.ReadAll(reader)
		return string(body), err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(a.root(), path)
	}
	body, err := os.ReadFile(path)
	return string(body), err
}
