package instruction

import (
	"context"
	"fmt"
	"strings"

	"github.com/neprel/git-a2a/internal/contact"
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
	default:
		return contact.Record{}, fmt.Errorf("instruction: unsupported contact kind %s", d.ContactKind)
	}
	return contact.Record{Agent: request.Agent, Kind: d.ContactKind, ID: id, State: "instruction", Instruction: text}, nil
}
