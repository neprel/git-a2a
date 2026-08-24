package declared

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/contact"
	"github.com/neprel/git-a2a/internal/manifest"
)

func TestHTTPRequiresConsentThenEscapesAndDelivers(t *testing.T) {
	message := "line \"quoted\" }], \"admin\":true"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("module") != "acme/lib & tools" {
			t.Errorf("query=%s", r.URL.RawQuery)
		}
		if r.Header.Get("X-Static") != "declared" || r.Header.Get("Authorization") != "" {
			t.Errorf("headers=%v", r.Header)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["message"] != message || payload["module"] != "acme/lib & tools" {
			t.Errorf("payload=%#v", payload)
		}
		fmt.Fprint(w, "ACME-42\n")
	}))
	defer server.Close()
	request := contact.Request{Intent: "change", Module: "acme/lib & tools", Origin: "https://git.example/acme/lib", Message: message, Contact: manifest.Contact{
		Kind: "http", URL: server.URL + "/issues?module={module}&intent={intent}", ContentType: "application/json",
		Headers: map[string]string{"X-Static": "declared"}, Body: `{"module":"{module}","message":"{message}"}`,
	}}
	record, err := (Driver{ContactKind: "http", Client: server.Client()}).Deliver(context.Background(), request)
	if err != nil || record.State != "instruction" || record.Driver != "instruction" || !strings.Contains(record.Instruction, "curl -X 'POST'") {
		t.Fatalf("record=%#v err=%v", record, err)
	}
	t.Log(record.String())

	consent := &manifest.ContactSettings{AllowHTTP: []string{server.URL}}
	record, err = (Driver{ContactKind: "http", Consent: consent, Client: server.Client()}).Deliver(context.Background(), request)
	if err != nil || record.State != "sent" || record.Driver != "http" || record.ID != "ACME-42" {
		t.Fatalf("record=%#v err=%v", record, err)
	}
	t.Log(record.String())
}

func TestHTTPFailureUsesSharedHTMLSuppression(t *testing.T) {
	body := "<html>secret</html>"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, body)
	}))
	defer server.Close()
	request := contact.Request{Contact: manifest.Contact{Kind: "http", URL: server.URL}}
	_, err := (Driver{ContactKind: "http", Consent: &manifest.ContactSettings{AllowHTTP: []string{server.URL}}, Client: server.Client()}).Deliver(context.Background(), request)
	if err == nil || strings.Contains(err.Error(), "<") || !strings.Contains(err.Error(), fmt.Sprintf("html response, %d bytes, suppressed", len(body))) {
		t.Fatalf("error=%v", err)
	}
}

func TestExecUsesArgvWithoutShellAndMCPRefuses(t *testing.T) {
	var executable string
	var args []string
	var stdin string
	driver := Driver{ContactKind: "exec", Consent: &manifest.ContactSettings{AllowExec: []string{"acme-tracker"}},
		LookPath: func(name string) (string, error) { return "/consumer/bin/" + name, nil },
		Run: func(_ context.Context, exe string, gotArgs []string, input string) ([]byte, error) {
			executable, args, stdin = exe, append([]string(nil), gotArgs...), input
			return []byte("ACME-7"), nil
		},
	}
	request := contact.Request{Intent: "change", Module: "acme-lib; touch PWNED", Message: "$(touch PWNED)", Contact: manifest.Contact{
		Kind: "exec", Command: []string{"acme-tracker", "create"}, Args: []string{"--module", "{module}"}, Stdin: "{message}",
	}}
	record, err := driver.Deliver(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if executable != "/consumer/bin/acme-tracker" || strings.Join(args, "|") != "create|--module|acme-lib; touch PWNED" || stdin != "$(touch PWNED)" || record.Driver != "exec:acme-tracker" {
		t.Fatalf("exe=%q args=%v stdin=%q record=%#v", executable, args, stdin, record)
	}

	_, err = (Driver{ContactKind: "exec", MCP: true, Consent: driver.Consent}).Deliver(context.Background(), request)
	if err == nil || !strings.Contains(err.Error(), "refused through MCP") {
		t.Fatalf("error=%v", err)
	}
	t.Log(err)
}
