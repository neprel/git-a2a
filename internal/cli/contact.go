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
	"github.com/neprel/git-a2a/internal/manifest"
	"github.com/neprel/git-a2a/internal/routing"
)

func (a *App) contact(args []string) int {
	id, intent, messagePath := "", "question", ""
	wait := false
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
	drivers := []contactcore.Driver{contacta2a.Driver{}, contactgithub.Driver{}}
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
