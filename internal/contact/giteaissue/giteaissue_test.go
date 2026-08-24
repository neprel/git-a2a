package giteaissue

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

func TestRESTCreatesIssueAndTeaIsPreferred(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/acme/lib/issues" || r.Header.Get("Authorization") != "token secret" {
			t.Errorf("path=%s authorization=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var payload struct {
			Labels []string `json:"labels"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if strings.Join(payload.Labels, ",") != "change,agent" {
			t.Errorf("labels=%v", payload.Labels)
		}
		fmt.Fprint(w, `{"number":9,"html_url":"https://codeberg.org/acme/lib/issues/9"}`)
	}))
	defer server.Close()
	driver := Driver{Client: server.Client(), LookPath: func(string) (string, error) { return "", errors.New("missing") }, Getenv: func(name string) string {
		if name == "FORGEJO_TOKEN" {
			return "secret"
		}
		return ""
	}}
	record, err := driver.Deliver(context.Background(), contact.Request{Contact: manifest.Contact{Repo: "acme/lib", Server: strings.TrimPrefix(server.URL, "https://"), Labels: []string{"change", "agent"}}, Message: "Change"})
	if err != nil || record.Driver != "gitea-rest" || !strings.HasSuffix(record.ID, "/9") {
		t.Fatalf("record=%#v err=%v", record, err)
	}

	called := false
	driver = Driver{LookPath: func(string) (string, error) { return "/fake/tea", nil }, Run: func(_ context.Context, _ string, args []string, _ string, _ []string) ([]byte, error) {
		called = true
		if !strings.Contains(strings.Join(args, " "), "--login codeberg.org") {
			t.Errorf("args=%v", args)
		}
		return []byte("https://codeberg.org/acme/lib/issues/10"), nil
	}}
	record, err = driver.Deliver(context.Background(), contact.Request{Contact: manifest.Contact{Repo: "acme/lib", Server: "codeberg.org"}, Message: "Change"})
	if err != nil || !called || record.Driver != "tea" {
		t.Fatalf("record=%#v err=%v", record, err)
	}
}

func TestRESTSanitizesFailure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "bad\x00 request")
	}))
	defer server.Close()
	driver := Driver{Client: server.Client(), LookPath: func(string) (string, error) { return "", errors.New("missing") }, Getenv: func(string) string { return "secret" }}
	_, err := driver.Deliver(context.Background(), contact.Request{Contact: manifest.Contact{Repo: "acme/lib", Server: strings.TrimPrefix(server.URL, "https://")}, Message: "Change"})
	if err == nil || strings.ContainsRune(err.Error(), '\x00') || !strings.Contains(err.Error(), "HTTP 400 Bad Request") {
		t.Fatalf("error=%v", err)
	}
}
