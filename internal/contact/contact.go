package contact

import (
	"context"
	"fmt"

	"github.com/neprel/git-a2a/internal/manifest"
)

type Request struct {
	Agent   string
	Contact manifest.Contact
	Message string
	Wait    bool
}

type Record struct {
	Agent       string
	Kind        string
	ID          string
	State       string
	Instruction string
}

func (r Record) String() string {
	value := fmt.Sprintf("agent=%q kind=%s id=%q state=%s", r.Agent, r.Kind, r.ID, r.State)
	if r.Instruction != "" {
		value += fmt.Sprintf(" instruction=%q", r.Instruction)
	}
	return value
}

type Driver interface {
	Kind() string
	Deliver(context.Context, Request) (Record, error)
}
