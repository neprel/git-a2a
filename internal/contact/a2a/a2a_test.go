package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
