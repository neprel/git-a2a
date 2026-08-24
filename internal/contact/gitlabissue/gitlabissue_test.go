package gitlabissue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/contact"
	"github.com/neprel/git-a2a/internal/manifest"
)

func TestRESTCreatesIssueForNestedProjectWithLabels(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.EscapedPath(); got != "/api/v4/projects/acme%2Fplatform%2Flib/issues" {
			t.Errorf("path = %q raw=%q", got, r.URL.RawPath)
		}
		if r.Header.Get("PRIVATE-TOKEN") != "secret" {
			t.Errorf("token header missing")
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["labels"] != "change,agent" || payload["title"] != "Change API" {
			t.Errorf("payload = %#v", payload)
		}
		fmt.Fprint(w, `{"iid":42,"web_url":"https://gitlab.example/acme/platform/lib/-/issues/42"}`)
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "https://")
	driver := Driver{Client: server.Client(), LookPath: func(string) (string, error) { return "", errors.New("missing") }, Getenv: func(name string) string {
		if name == "GITLAB_TOKEN" {
			return "secret"
		}
		return ""
	}}
	record, err := driver.Deliver(context.Background(), contact.Request{Agent: "owner", Message: "# Change API\nDetails", Contact: manifest.Contact{Kind: "gitlab-issue", Repo: "acme/platform/lib", Server: host, Labels: []string{"change", "agent"}}})
	if err != nil {
		t.Fatal(err)
	}
	if record.Driver != "gitlab-rest" || record.State != "created" || !strings.HasSuffix(record.ID, "/42") {
		t.Fatalf("record = %#v", record)
	}
	t.Log(record.String())
}

func TestRESTSuppressesHTMLAndCLIIsPreferred(t *testing.T) {
	body := "<html><script>alert(1)</script></html>"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, body)
	}))
	defer server.Close()
	driver := Driver{Client: server.Client(), LookPath: func(string) (string, error) { return "", errors.New("missing") }, Getenv: func(string) string { return "secret" }}
	_, err := driver.Deliver(context.Background(), contact.Request{Contact: manifest.Contact{Repo: "acme/lib", Server: strings.TrimPrefix(server.URL, "https://")}, Message: "change"})
	if err == nil || strings.Contains(err.Error(), "<") || !strings.Contains(err.Error(), fmt.Sprintf("html response, %d bytes, suppressed", len(body))) {
		t.Fatalf("error = %v", err)
	}

	called := false
	driver = Driver{LookPath: func(name string) (string, error) { return "/fake/glab", nil }, Run: func(_ context.Context, executable string, args []string, stdin string, env []string) ([]byte, error) {
		called = true
		joined := strings.Join(args, " ") + strings.Join(env, " ")
		if executable != "/fake/glab" || !strings.Contains(joined, "--label bug") || !strings.Contains(joined, "GLAB_HOST=gitlab.example") || stdin != "Fix" {
			t.Errorf("exe=%s args=%v stdin=%q", executable, args, stdin)
		}
		return []byte("created\nhttps://gitlab.example/acme/lib/-/issues/7\n"), nil
	}}
	record, err := driver.Deliver(context.Background(), contact.Request{Contact: manifest.Contact{Repo: "acme/lib", Server: "gitlab.example", Labels: []string{"bug"}}, Message: "Fix"})
	if err != nil || !called || record.Driver != "glab" {
		t.Fatalf("record=%#v err=%v", record, err)
	}
}
