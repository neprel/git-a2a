package githubissue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/neprel/git-a2a/internal/contact"
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
	args := []string{"issue", "create", "--repo", request.Contact.Repo, "--title", title(request.Message), "--body-file", "-"}
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
	return contact.Record{Agent: request.Agent, Kind: d.Kind(), ID: id, State: "created"}, nil
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
		return contact.Record{}, fmt.Errorf("github-issue: gh is unavailable and GH_TOKEN or GITHUB_TOKEN is not set")
	}
	apiURL := strings.TrimSuffix(getenv("GITHUB_API_URL"), "/")
	if apiURL == "" {
		apiURL = "https://api.github.com"
	}
	parsed, err := url.Parse(apiURL)
	if err != nil {
		return contact.Record{}, fmt.Errorf("github-issue: API URL: %w", err)
	}
	parsed.Path = path.Join(parsed.Path, "repos", request.Contact.Repo, "issues")
	payload, _ := json.Marshal(map[string]any{"title": title(request.Message), "body": request.Message, "labels": request.Contact.Labels})
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
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return contact.Record{}, fmt.Errorf("github-issue: REST HTTP %s: %s", response.Status, strings.TrimSpace(string(body)))
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
	return contact.Record{Agent: request.Agent, Kind: d.Kind(), ID: id, State: "created"}, nil
}

func title(message string) string {
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		if line == "" {
			continue
		}
		if len(line) > 80 {
			return line[:77] + "..."
		}
		return line
	}
	return "git-a2a contact request"
}

func runCommand(ctx context.Context, executable string, args []string, input string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
