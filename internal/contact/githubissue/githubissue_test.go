package githubissue

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

func TestDeliverUsesFakeGHWhenAvailable(t *testing.T) {
	var gotExecutable, gotInput string
	var gotArgs []string
	driver := Driver{
		LookPath: func(name string) (string, error) {
			if name != "gh" {
				t.Fatalf("lookup=%q", name)
			}
			return "/fake/gh", nil
		},
		Run: func(_ context.Context, executable string, args []string, input string) ([]byte, error) {
			gotExecutable, gotArgs, gotInput = executable, args, input
			return []byte("https://github.com/acme/lib/issues/17\n"), nil
		},
	}
	record, err := driver.Deliver(context.Background(), contact.Request{
		Agent: "owner", Message: "# Change API\nDetails", Contact: manifest.Contact{Kind: "github-issue", Repo: "acme/lib", Labels: []string{"agent", "change"}},
	})
	if err != nil || gotExecutable != "/fake/gh" || gotInput != "# Change API\nDetails" {
		t.Fatalf("record=%#v executable=%q input=%q err=%v", record, gotExecutable, gotInput, err)
	}
	joined := strings.Join(gotArgs, " ")
	if joined != "issue create --repo acme/lib --title Change API --body-file - --label agent --label change" {
		t.Fatalf("args=%q", joined)
	}
	if record.ID != "https://github.com/acme/lib/issues/17" || record.State != "created" {
		t.Fatalf("record=%#v", record)
	}
}

func TestDeliverFallsBackToREST(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/lib/issues" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("request=%s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body struct {
			Title  string   `json:"title"`
			Body   string   `json:"body"`
			Labels []string `json:"labels"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Title != "Fix it" || body.Body != "Fix it\nPlease" || strings.Join(body.Labels, ",") != "bug" {
			t.Errorf("body=%#v", body)
		}
		fmt.Fprint(w, `{"html_url":"https://github.com/acme/lib/issues/19","number":19}`)
	}))
	defer server.Close()
	driver := Driver{
		Client:   server.Client(),
		LookPath: func(string) (string, error) { return "", errors.New("missing") },
		Getenv: func(name string) string {
			switch name {
			case "GH_TOKEN":
				return "secret"
			case "GITHUB_API_URL":
				return server.URL
			default:
				return ""
			}
		},
	}
	record, err := driver.Deliver(context.Background(), contact.Request{
		Agent: "owner", Message: "Fix it\nPlease", Contact: manifest.Contact{Kind: "github-issue", Repo: "acme/lib", Labels: []string{"bug"}},
	})
	if err != nil || record.ID != "https://github.com/acme/lib/issues/19" {
		t.Fatalf("record=%#v err=%v", record, err)
	}
}
