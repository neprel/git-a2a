package cli

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/neprel/git-a2a/internal/cache"
	contactcore "github.com/neprel/git-a2a/internal/contact"
	contacta2a "github.com/neprel/git-a2a/internal/contact/a2a"
	contactdeclared "github.com/neprel/git-a2a/internal/contact/declared"
	contactemail "github.com/neprel/git-a2a/internal/contact/email"
	contactgitea "github.com/neprel/git-a2a/internal/contact/giteaissue"
	contactgithub "github.com/neprel/git-a2a/internal/contact/githubissue"
	contactgitlab "github.com/neprel/git-a2a/internal/contact/gitlabissue"
	contactinstruction "github.com/neprel/git-a2a/internal/contact/instruction"
	contactplugin "github.com/neprel/git-a2a/internal/contact/plugin"
	lockfile "github.com/neprel/git-a2a/internal/lock"
	"github.com/neprel/git-a2a/internal/manifest"
	"github.com/neprel/git-a2a/internal/routing"
)

func (a *App) contact(args []string) int {
	id, intent, messagePath := "", "question", ""
	wait, externalOK, listDrivers, dryRun := false, false, false, false
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
		case "--list-drivers":
			listDrivers = true
		case "--dry-run":
			dryRun = true
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
	if listDrivers && id == "" {
		return a.listContactDrivers(nil, nil)
	}
	if id == "" || (!listDrivers && messagePath == "") {
		fmt.Fprintln(a.Err, "contact: module id and --message FILE|- are required")
		return 2
	}
	if locked, lockErr := lockfile.Load(a.root()); lockErr == nil {
		if entry, ok := locked.Dependencies[id]; ok && entry.Manifest == "none" {
			fmt.Fprintf(a.Err, "no agents declared: %s is not an a2a module\n", id)
			return 2
		}
	}
	message := ""
	var err error
	if !listDrivers {
		message, err = a.readContactMessage(messagePath)
		if err != nil {
			fmt.Fprintf(a.Err, "contact: message: %v\n", err)
			return 2
		}
		if strings.TrimSpace(message) == "" {
			fmt.Fprintln(a.Err, "contact: message is empty")
			return 2
		}
	}
	m, err := manifest.LoadDir(cache.Dir(a.root(), id))
	if err != nil {
		fmt.Fprintf(a.Err, "contact: %v\n", err)
		return 2
	}
	matches, role := routing.Resolve(m, intent, "")
	consumer, ownErr := manifest.LoadDir(a.root())
	if ownErr != nil {
		consumer = nil
		if matchesDeclineExternal(matches) {
			fmt.Fprintf(a.Err, "contact: own manifest: %v\n", ownErr)
			return 2
		}
	}
	if listDrivers {
		return a.listContactDrivers(m, consumer)
	}
	if externalRequestRefused(consumer, m, matches) && !externalOK {
		fmt.Fprintln(a.Err, "contact: owner does not accept external requests; pass --external-ok only after human approval")
		return 2
	}
	for _, match := range matches {
		for _, declared := range match.Contacts {
			if a.mcpInvocation && declared.Kind == "exec" {
				fmt.Fprintln(a.Err, "contact: exec contact: refused through MCP; run git-a2a contact from an approved CLI session")
				return 1
			}
			driver := a.contactDriver(declared, consumer)
			record, deliveryErr := driver.Deliver(a.context(), contactcore.Request{
				Agent: match.Agent.Name, Intent: intent, Module: m.Module.ID, Origin: m.Module.Repository,
				Contact: declared, Message: message, Wait: wait, DryRun: dryRun,
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
	if len(matches) == 0 {
		fmt.Fprintf(a.Err, "contact: nobody is declared for %q on %s; routed role is %s\n", intent, id, role)
	} else {
		fmt.Fprintf(a.Err, "contact: no supported delivery kind is declared for %q on %s\n", intent, id)
	}
	return 2
}

func (a *App) contactDriver(declared manifest.Contact, consumer *manifest.Manifest) contactcore.Driver {
	if executable, err := contactplugin.Find(declared.Kind); err == nil {
		return contactplugin.Driver{ContactKind: declared.Kind, Executable: executable, Stderr: a.Err}
	}
	var consent *manifest.ContactSettings
	if consumer != nil && consumer.Settings != nil {
		consent = consumer.Settings.Contact
	}
	switch declared.Kind {
	case "a2a":
		return contacta2a.Driver{Client: a.HTTPClient}
	case "github-issue":
		return contactgithub.Driver{Client: a.HTTPClient}
	case "gitlab-issue":
		return contactgitlab.Driver{Client: a.HTTPClient}
	case "gitea-issue":
		return contactgitea.Driver{Client: a.HTTPClient}
	case "email":
		return contactemail.Driver{}
	case "http", "exec":
		return contactdeclared.Driver{ContactKind: declared.Kind, Consent: consent, MCP: a.mcpInvocation, Client: a.HTTPClient}
	default:
		return contactinstruction.Driver{ContactKind: declared.Kind}
	}
}

func (a *App) listContactDrivers(owner, consumer *manifest.Manifest) int {
	contacts := map[string]manifest.Contact{}
	if owner == nil {
		for _, kind := range []string{"a2a", "github-issue", "gitlab-issue", "gitea-issue", "bitbucket-issue", "azure-boards", "http", "exec", "email", "url"} {
			contacts[kind] = manifest.Contact{Kind: kind}
		}
	} else {
		for _, agent := range owner.Agents {
			for _, declared := range agent.Contacts {
				contacts[declared.Kind] = declared
			}
		}
	}
	kinds := make([]string, 0, len(contacts))
	for kind := range contacts {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		driver, reason := a.contactDriverDescription(contacts[kind], consumer)
		fmt.Fprintf(a.Out, "kind=%s driver=%s reason=%q\n", kind, driver, reason)
	}
	return 0
}

func (a *App) contactDriverDescription(declared manifest.Contact, consumer *manifest.Manifest) (string, string) {
	if executable, err := contactplugin.Find(declared.Kind); err == nil {
		return "plugin:" + filepath.Base(executable), "consumer plugin found on PATH"
	}
	switch declared.Kind {
	case "a2a":
		return "a2a", "built-in JSON-RPC driver"
	case "github-issue":
		if _, err := exec.LookPath("gh"); err == nil {
			return "gh", "GitHub CLI found on PATH"
		}
		if os.Getenv("GH_TOKEN") != "" || os.Getenv("GITHUB_TOKEN") != "" {
			return "github-rest", "GitHub token available in consumer environment"
		}
	case "gitlab-issue":
		if _, err := exec.LookPath("glab"); err == nil {
			return "glab", "GitLab CLI found on PATH"
		}
		if os.Getenv("GITLAB_TOKEN") != "" || os.Getenv("GLAB_TOKEN") != "" {
			return "gitlab-rest", "GitLab token available in consumer environment"
		}
	case "gitea-issue":
		if _, err := exec.LookPath("tea"); err == nil {
			return "tea", "Gitea CLI found on PATH"
		}
		if os.Getenv("GITEA_TOKEN") != "" || os.Getenv("FORGEJO_TOKEN") != "" {
			return "gitea-rest", "Gitea or Forgejo token available in consumer environment"
		}
	case "email":
		if _, err := exec.LookPath("sendmail"); err == nil {
			return "sendmail", "sendmail found on consumer PATH"
		}
		if os.Getenv("GITA2A_SMTP_URL") != "" && os.Getenv("GITA2A_SMTP_PASSWORD") != "" {
			return "smtp", "consumer SMTP environment is configured"
		}
	case "http":
		if consumerAllowsHTTP(consumer, declared.URL) {
			return "http", "origin allowed by consumer settings.contact.allow-http"
		}
	case "exec":
		if len(declared.Command) > 0 && consumerAllowsExec(consumer, declared.Command[0]) {
			if _, err := exec.LookPath(declared.Command[0]); err == nil {
				return "exec:" + declared.Command[0], "binary allowed by consumer and found on PATH"
			}
		}
	}
	return "instruction", "delivery credentials, executable, or consent unavailable"
}

func consumerAllowsHTTP(consumer *manifest.Manifest, target string) bool {
	if consumer == nil || consumer.Settings == nil || consumer.Settings.Contact == nil {
		return false
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return false
	}
	want := parsed.Scheme + "://" + parsed.Host
	for _, allowed := range consumer.Settings.Contact.AllowHTTP {
		if allowed == want {
			return true
		}
	}
	return false
}

func consumerAllowsExec(consumer *manifest.Manifest, binary string) bool {
	if consumer == nil || consumer.Settings == nil || consumer.Settings.Contact == nil {
		return false
	}
	for _, allowed := range consumer.Settings.Contact.AllowExec {
		if allowed == binary {
			return true
		}
	}
	return false
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
