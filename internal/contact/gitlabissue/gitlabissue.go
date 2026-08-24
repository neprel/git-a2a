package gitlabissue

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

func (Driver) Kind() string { return "gitlab-issue" }

func (d Driver) Deliver(ctx context.Context, request contact.Request) (contact.Record, error) {
	lookPath := d.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if executable, err := lookPath("glab"); err == nil {
		return d.deliverCLI(ctx, executable, request)
	}
	return d.deliverREST(ctx, request)
}

func (d Driver) deliverCLI(ctx context.Context, executable string, request contact.Request) (contact.Record, error) {
	args := []string{"issue", "create", "--repo", request.Contact.Repo, "--title", forge.Title(request.Message), "--description", "-"}
	for _, label := range request.Contact.Labels {
		args = append(args, "--label", label)
	}
	server := request.Contact.Server
	if server == "" {
		server = "gitlab.com"
	}
	environment := append(os.Environ(), "GITLAB_HOST="+server, "GLAB_HOST="+server)
	run := d.Run
	if run == nil {
		run = forge.Run
	}
	output, err := run(ctx, executable, args, forge.Body(request.Message), environment)
	if err != nil {
		return contact.Record{}, fmt.Errorf("gitlab-issue: glab: %w", err)
	}
	id := lastNonEmptyLine(string(output))
	if id == "" {
		return contact.Record{}, fmt.Errorf("gitlab-issue: glab returned no issue URL")
	}
	return contact.Record{Agent: request.Agent, Kind: d.Kind(), Driver: "glab", ID: id, State: "created", URL: id}, nil
}

func (d Driver) deliverREST(ctx context.Context, request contact.Request) (contact.Record, error) {
	getenv := d.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	token := getenv("GITLAB_TOKEN")
	if token == "" {
		token = getenv("GLAB_TOKEN")
	}
	if token == "" {
		return forge.Instruction(request.Agent, d.Kind(), request.Contact, request.Message), nil
	}
	server := strings.TrimSuffix(request.Contact.Server, "/")
	if server == "" {
		server = "gitlab.com"
	}
	server = strings.TrimPrefix(server, "https://")
	endpoint := "https://" + server + "/api/v4/projects/" + url.PathEscape(request.Contact.Repo) + "/issues"
	payload, _ := json.Marshal(map[string]any{
		"title": forge.Title(request.Message), "description": forge.Body(request.Message),
		"labels": strings.Join(request.Contact.Labels, ","),
	})
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return contact.Record{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("PRIVATE-TOKEN", token)
	client := d.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return contact.Record{}, fmt.Errorf("gitlab-issue: REST: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return contact.Record{}, fmt.Errorf("gitlab-issue: REST %s", remotehttp.ErrorResponse(response))
	}
	var created struct {
		IID int    `json:"iid"`
		URL string `json:"web_url"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		return contact.Record{}, fmt.Errorf("gitlab-issue: REST response: %w", err)
	}
	id := created.URL
	if id == "" && created.IID != 0 {
		id = fmt.Sprintf("%s#%d", request.Contact.Repo, created.IID)
	}
	if id == "" {
		return contact.Record{}, fmt.Errorf("gitlab-issue: REST returned no issue identifier")
	}
	return contact.Record{Agent: request.Agent, Kind: d.Kind(), Driver: "gitlab-rest", ID: id, State: "created", URL: created.URL}, nil
}

func lastNonEmptyLine(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if value := strings.TrimSpace(lines[i]); value != "" {
			return value
		}
	}
	return ""
}
