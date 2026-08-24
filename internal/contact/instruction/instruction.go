package instruction

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/neprel/git-a2a/internal/contact"
	"github.com/neprel/git-a2a/internal/contact/forge"
)

type Driver struct {
	ContactKind string
}

func (d Driver) Kind() string { return d.ContactKind }

func (d Driver) Deliver(_ context.Context, request contact.Request) (contact.Record, error) {
	id, text := "", ""
	switch d.ContactKind {
	case "url":
		id = request.Contact.URL
		text = "open " + request.Contact.URL + " and send the supplied message"
	case "email":
		id = request.Contact.Address
		text = "email the supplied message to " + request.Contact.Address
		if request.Contact.SubjectPrefix != "" {
			text += " with subject prefix " + request.Contact.SubjectPrefix
		}
	case "mattermost", "slack", "discord", "telegram", "teams":
		parts := []string{d.ContactKind}
		if request.Contact.Server != "" {
			parts = append(parts, "server="+request.Contact.Server)
		}
		if request.Contact.Channel != "" {
			parts = append(parts, "channel="+request.Contact.Channel)
		}
		if request.Contact.Handle != "" {
			parts = append(parts, "handle="+request.Contact.Handle)
		}
		id = strings.Join(parts, " ")
		text = "send the supplied message via " + id
	case "github-issue", "gitlab-issue", "gitea-issue", "bitbucket-issue":
		return forge.Instruction(request.Agent, d.ContactKind, request.Contact, request.Message), nil
	case "azure-boards":
		id = request.Contact.Organization + "/" + request.Contact.Project
		text = fmt.Sprintf("run az boards work-item create --org %s --project %s --type %s --title %s",
			quote(request.Contact.Organization), quote(request.Contact.Project), quote(request.Contact.IssueType), quote(forge.Title(request.Message)))
	case "jira":
		id = request.Contact.URL
		text = "create an issue in " + request.Contact.Project + " at " + request.Contact.URL
	default:
		id = d.ContactKind
		text = "deliver the supplied message using declared contact kind " + d.ContactKind
	}
	return contact.Record{Agent: request.Agent, Kind: d.ContactKind, Driver: "instruction", ID: id, State: "instruction", Instruction: text}, nil
}

func quote(value string) string { return url.QueryEscape(value) }
