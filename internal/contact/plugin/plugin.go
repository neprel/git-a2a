package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
	"unicode"

	"github.com/neprel/git-a2a/internal/contact"
)

const protocolVersion = 1

type Driver struct {
	ContactKind string
	Executable  string
	Timeout     time.Duration
	Stderr      io.Writer
}

type requestEnvelope struct {
	Protocol int    `json:"protocol"`
	Kind     string `json:"kind"`
	Intent   string `json:"intent"`
	Module   string `json:"module"`
	Origin   string `json:"origin"`
	Contact  any    `json:"contact"`
	Message  string `json:"message"`
	DryRun   bool   `json:"dry-run"`
}

type responseEnvelope struct {
	ID    string `json:"id"`
	State string `json:"state"`
	URL   string `json:"url,omitempty"`
	Note  string `json:"note,omitempty"`
}

func Name(kind string) string { return "git-a2a-contact-" + kind }

func Find(kind string) (string, error) {
	if strings.TrimSpace(kind) == "" || strings.ContainsAny(kind, `/\\`) {
		return "", fmt.Errorf("contact kind cannot name a PATH plugin")
	}
	return exec.LookPath(Name(kind))
}

func (d Driver) Kind() string { return d.ContactKind }

func (d Driver) Deliver(ctx context.Context, request contact.Request) (contact.Record, error) {
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	pluginContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body, err := json.Marshal(requestEnvelope{
		Protocol: protocolVersion, Kind: d.ContactKind, Intent: request.Intent, Module: request.Module,
		Origin: request.Origin, Contact: request.Contact, Message: request.Message, DryRun: request.DryRun,
	})
	if err != nil {
		return contact.Record{}, fmt.Errorf("plugin %s: request: %w", Name(d.ContactKind), err)
	}
	command := exec.CommandContext(pluginContext, d.Executable)
	command.Stdin = bytes.NewReader(append(body, '\n'))
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	d.forwardStderr(stderr.String())
	if pluginContext.Err() == context.DeadlineExceeded {
		return contact.Record{}, fmt.Errorf("plugin %s: timed out after %s", Name(d.ContactKind), timeout)
	}
	if err != nil {
		exitCode := 1
		var exitError *exec.ExitError
		if ok := errors.As(err, &exitError); ok {
			exitCode = exitError.ExitCode()
		}
		if exitCode == 2 {
			return contact.Record{}, fmt.Errorf("plugin %s: refused request", Name(d.ContactKind))
		}
		return contact.Record{}, fmt.Errorf("plugin %s: failed (exit %d)", Name(d.ContactKind), exitCode)
	}
	var result responseEnvelope
	decoder := json.NewDecoder(&stdout)
	if err := decoder.Decode(&result); err != nil {
		return contact.Record{}, fmt.Errorf("plugin %s: invalid response: %w", Name(d.ContactKind), err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return contact.Record{}, fmt.Errorf("plugin %s: response must contain one JSON object", Name(d.ContactKind))
	}
	if result.State != "created" && result.State != "sent" && result.State != "instruction" {
		return contact.Record{}, fmt.Errorf("plugin %s: invalid state %q", Name(d.ContactKind), result.State)
	}
	return contact.Record{
		Agent: request.Agent, Kind: d.ContactKind, Driver: "plugin:" + Name(d.ContactKind),
		ID: result.ID, State: result.State, URL: result.URL, Note: result.Note,
	}, nil
}

func (d Driver) forwardStderr(value string) {
	if d.Stderr == nil {
		return
	}
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			fmt.Fprintf(d.Stderr, "plugin %s: %s\n", Name(d.ContactKind), line)
		}
	}
}
