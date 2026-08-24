package githubissue

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
	Run      func(context.Context, string, []string, string) ([]byte, error)
	Getenv   func(string) string
}

func (Driver) Kind() string { return "github-issue" }

func (d Driver) Deliver(ctx context.Context, request contact.Request) (contact.Record, error) {
	lookPath := d.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if executable, err := lookPath("gh"); err == nil {
		return d.deliverGH(ctx, executable, request)
	}
	return d.deliverREST(ctx, request)
}

func (d Driver) deliverGH(ctx context.Context, executable string, request contact.Request) (contact.Record, error) {
	repository := request.Contact.Repo
	if request.Contact.Server != "" && request.Contact.Server != "github.com" {
		repository = strings.TrimPrefix(strings.TrimPrefix(request.Contact.Server, "https://"), "http://") + "/" + repository
	}
	args := []string{"issue", "create", "--repo", repository, "--title", forge.Title(request.Message), "--body-file", "-"}
	for _, label := range request.Contact.Labels {
		args = append(args, "--label", label)
	}
	run := d.Run
	if run == nil {
		run = runCommand
	}
	output, err := run(ctx, executable, args, request.Message)
	if err != nil {
		return contact.Record{}, fmt.Errorf("github-issue: gh: %w", err)
	}
	id := strings.TrimSpace(string(output))
	if id == "" {
		return contact.Record{}, fmt.Errorf("github-issue: gh returned no issue URL")
	}
	return contact.Record{Agent: request.Agent, Kind: d.Kind(), Driver: "gh", ID: id, State: "created", URL: id}, nil
}

func (d Driver) deliverREST(ctx context.Context, request contact.Request) (contact.Record, error) {
	getenv := d.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	token := getenv("GH_TOKEN")
	if token == "" {
		token = getenv("GITHUB_TOKEN")
	}
	if token == "" {
		return forge.Instruction(request.Agent, d.Kind(), request.Contact, request.Message), nil
	}
	apiURL := strings.TrimSuffix(getenv("GITHUB_API_URL"), "/")
	if apiURL == "" {
		server := strings.TrimSuffix(request.Contact.Server, "/")
		if server == "" || server == "github.com" {
			apiURL = "https://api.github.com"
		} else if strings.HasPrefix(server, "https://") {
			apiURL = server + "/api/v3"
		} else {
			apiURL = "https://" + server + "/api/v3"
		}
	}
	parsed, err := url.Parse(apiURL)
	if err != nil {
		return contact.Record{}, fmt.Errorf("github-issue: API URL: %w", err)
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/repos/" + request.Contact.Repo + "/issues"
	payload, _ := json.Marshal(map[string]any{"title": forge.Title(request.Message), "body": forge.Body(request.Message), "labels": request.Contact.Labels})
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(payload))
	if err != nil {
		return contact.Record{}, err
	}
	httpRequest.Header.Set("Accept", "application/vnd.github+json")
	httpRequest.Header.Set("Authorization", "Bearer "+token)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	client := d.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return contact.Record{}, fmt.Errorf("github-issue: REST: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return contact.Record{}, fmt.Errorf("github-issue: REST %s", remotehttp.ErrorResponse(response))
	}
	var created struct {
		URL    string `json:"html_url"`
		Number int    `json:"number"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		return contact.Record{}, fmt.Errorf("github-issue: REST response: %w", err)
	}
	id := created.URL
	if id == "" && created.Number != 0 {
		id = fmt.Sprintf("%s#%d", request.Contact.Repo, created.Number)
	}
	if id == "" {
		return contact.Record{}, fmt.Errorf("github-issue: REST returned no issue identifier")
	}
	return contact.Record{Agent: request.Agent, Kind: d.Kind(), Driver: "github-rest", ID: id, State: "created", URL: created.URL}, nil
}

func runCommand(ctx context.Context, executable string, args []string, input string) ([]byte, error) {
	return forge.Run(ctx, executable, args, input, nil)
}
