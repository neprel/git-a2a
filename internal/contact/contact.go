package contact

import (
	"context"
	"fmt"

	"github.com/neprel/git-a2a/internal/manifest"
)

type Request struct {
	Agent   string
	Intent  string
	Module  string
	Origin  string
	Contact manifest.Contact
	Message string
	Wait    bool
	DryRun  bool
}

type Record struct {
	Agent       string
	Kind        string
	ID          string
	State       string
	Driver      string
	URL         string
	Note        string
	Instruction string
}

func (r Record) String() string {
	value := fmt.Sprintf("agent=%q kind=%s id=%q state=%s", r.Agent, r.Kind, r.ID, r.State)
	if r.Instruction != "" {
		value += fmt.Sprintf(" instruction=%q", r.Instruction)
	}
	if r.Driver != "" {
		value += fmt.Sprintf(" driver=%s", r.Driver)
	}
	if r.URL != "" {
		value += fmt.Sprintf(" url=%q", r.URL)
	}
	if r.Note != "" {
		value += fmt.Sprintf(" note=%q", r.Note)
	}
	return value
}

type Driver interface {
	Kind() string
	Deliver(context.Context, Request) (Record, error)
}
