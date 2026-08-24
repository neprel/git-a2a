package a2a

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

func TestDeliverSendMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("A2A-Version"); got != "1.0" {
			t.Errorf("A2A-Version=%q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if body["jsonrpc"] != "2.0" || body["method"] != "SendMessage" {
			t.Errorf("request=%#v", body)
		}
		params := body["params"].(map[string]any)
		message := params["message"].(map[string]any)
		parts := message["parts"].([]any)
		if message["role"] != "ROLE_USER" || parts[0].(map[string]any)["text"] != "please review" {
			t.Errorf("message=%#v", message)
		}
		fmt.Fprint(w, `{"jsonrpc":"2.0","result":{"task":{"id":"task-7","status":{"state":"TASK_STATE_SUBMITTED"}}}}`)
	}))
	defer server.Close()
	record, err := (Driver{Client: server.Client()}).Deliver(context.Background(), contact.Request{
		Agent: "owner", Contact: manifest.Contact{Kind: "a2a", URL: server.URL}, Message: "please review",
	})
	if err != nil || record.ID != "task-7" || record.State != "TASK_STATE_SUBMITTED" {
		t.Fatalf("record=%#v err=%v", record, err)
	}
}

func TestDeliverBoundsAndSanitizesHTTPErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, "<html>failure\x00\r\n"+strings.Repeat("x", 240)+"</html>")
	}))
	defer server.Close()
	_, err := (Driver{Client: server.Client()}).Deliver(context.Background(), contact.Request{
		Agent: "owner", Contact: manifest.Contact{Kind: "a2a", URL: server.URL}, Message: "please review",
	})
	if err == nil {
		t.Fatal("non-2xx response unexpectedly succeeded")
	}
	const prefix = "a2a: HTTP 502 Bad Gateway: "
	if !strings.HasPrefix(err.Error(), prefix) {
		t.Fatalf("error=%q, want prefix %q", err, prefix)
	}
	excerpt := strings.TrimPrefix(err.Error(), prefix)
	if got := len([]rune(excerpt)); got != 200 {
		t.Fatalf("excerpt length=%d, want 200: %q", got, excerpt)
	}
	if strings.ContainsAny(excerpt, "\x00\r\n") || !strings.HasPrefix(excerpt, "<html>failure") {
		t.Fatalf("excerpt was not sanitized: %q", excerpt)
	}
}

func TestDeliverWaitConsumesStreamAndReportsFinalState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["method"] != "SendStreamingMessage" {
			t.Errorf("method=%v", body["method"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"jsonrpc":"2.0","result":{"task":{"id":"task-8","status":{"state":"TASK_STATE_WORKING"}}}}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `data: {"jsonrpc":"2.0","result":{"statusUpdate":{"taskId":"task-8","status":{"state":"TASK_STATE_COMPLETED"}}}}`)
		fmt.Fprintln(w)
	}))
	defer server.Close()
	record, err := (Driver{Client: server.Client()}).Deliver(context.Background(), contact.Request{
		Agent: "owner", Contact: manifest.Contact{Kind: "a2a", URL: server.URL}, Message: "wait", Wait: true,
	})
	if err != nil || record.ID != "task-8" || record.State != "TASK_STATE_COMPLETED" {
		t.Fatalf("record=%#v err=%v", record, err)
	}
}
