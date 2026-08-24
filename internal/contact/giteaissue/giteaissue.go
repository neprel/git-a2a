package giteaissue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/neprel/git-a2a/internal/contact"
	"github.com/neprel/git-a2a/internal/contact/forge"
	"github.com/neprel/git-a2a/internal/remotehttp"
)

type Driver struct {
	Client   *http.Client
	LookPath func(string) (string, error)
	Run      func(context.Context, string, []string, string, []string) ([]byte, error)
	Getenv   func(string) string
}

func (Driver) Kind() string { return "gitea-issue" }

func (d Driver) Deliver(ctx context.Context, request contact.Request) (contact.Record, error) {
	lookPath := d.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if executable, err := lookPath("tea"); err == nil {
		return d.deliverCLI(ctx, executable, request)
	}
	return d.deliverREST(ctx, request)
}

func (d Driver) deliverCLI(ctx context.Context, executable string, request contact.Request) (contact.Record, error) {
	args := []string{"issues", "create", "--repo", request.Contact.Repo, "--login", request.Contact.Server, "--title", forge.Title(request.Message), "--body", forge.Body(request.Message)}
	for _, label := range request.Contact.Labels {
		args = append(args, "--labels", label)
	}
	run := d.Run
	if run == nil {
		run = forge.Run
	}
	output, err := run(ctx, executable, args, "", os.Environ())
	if err != nil {
		return contact.Record{}, fmt.Errorf("gitea-issue: tea: %w", err)
	}
	id := strings.TrimSpace(string(output))
	if id == "" {
		return contact.Record{}, fmt.Errorf("gitea-issue: tea returned no issue identifier")
	}
	return contact.Record{Agent: request.Agent, Kind: d.Kind(), Driver: "tea", ID: id, State: "created", URL: id}, nil
}

func (d Driver) deliverREST(ctx context.Context, request contact.Request) (contact.Record, error) {
	getenv := d.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	token := getenv("GITEA_TOKEN")
	if token == "" {
		token = getenv("FORGEJO_TOKEN")
	}
	if token == "" {
		return forge.Instruction(request.Agent, d.Kind(), request.Contact, request.Message), nil
	}
	parts := strings.Split(strings.Trim(request.Contact.Repo, "/"), "/")
	if len(parts) != 2 {
		return contact.Record{}, fmt.Errorf("gitea-issue: repo must be owner/name")
	}
	server := strings.TrimSuffix(strings.TrimPrefix(request.Contact.Server, "https://"), "/")
	endpoint := "https://" + server + "/api/v1/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/issues"
	payload, _ := json.Marshal(map[string]any{
		"title": forge.Title(request.Message), "body": forge.Body(request.Message), "labels": request.Contact.Labels,
	})
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return contact.Record{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "token "+token)
	client := d.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return contact.Record{}, fmt.Errorf("gitea-issue: REST: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return contact.Record{}, fmt.Errorf("gitea-issue: REST %s", remotehttp.ErrorResponse(response))
	}
	var created struct {
		ID     int    `json:"id"`
		Number int    `json:"number"`
		URL    string `json:"html_url"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		return contact.Record{}, fmt.Errorf("gitea-issue: REST response: %w", err)
	}
	id := created.URL
	if id == "" && created.Number != 0 {
		id = fmt.Sprintf("%s#%d", request.Contact.Repo, created.Number)
	}
	if id == "" && created.ID != 0 {
		id = fmt.Sprintf("%s#%d", request.Contact.Repo, created.ID)
	}
	if id == "" {
		return contact.Record{}, fmt.Errorf("gitea-issue: REST returned no issue identifier")
	}
	return contact.Record{Agent: request.Agent, Kind: d.Kind(), Driver: "gitea-rest", ID: id, State: "created", URL: created.URL}, nil
}
