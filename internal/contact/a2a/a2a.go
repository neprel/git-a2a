package a2a

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/neprel/git-a2a/internal/contact"
)

type Driver struct {
	Client *http.Client
}

func (Driver) Kind() string { return "a2a" }

func (d Driver) Deliver(ctx context.Context, request contact.Request) (contact.Record, error) {
	method := "SendMessage"
	if request.Wait {
		method = "SendStreamingMessage"
	}
	messageID, err := newID()
	if err != nil {
		return contact.Record{}, fmt.Errorf("a2a: create message id: %w", err)
	}
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      messageID,
		"method":  method,
		"params": map[string]any{
			"message": map[string]any{
				"messageId": messageID,
				"role":      "ROLE_USER",
				"parts":     []map[string]string{{"text": request.Message}},
			},
			"configuration": map[string]any{"returnImmediately": !request.Wait},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return contact.Record{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, request.Contact.URL, bytes.NewReader(body))
	if err != nil {
		return contact.Record{}, fmt.Errorf("a2a: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json, text/event-stream")
	httpRequest.Header.Set("A2A-Version", "1.0")
	client := d.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return contact.Record{}, fmt.Errorf("a2a: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return contact.Record{}, fmt.Errorf("a2a: HTTP %s: %s", response.Status, strings.TrimSpace(string(limited)))
	}
	result := rpcResult{}
	if request.Wait || strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		result, err = decodeStream(response.Body)
	} else {
		err = json.NewDecoder(response.Body).Decode(&result)
		if err == nil {
			err = result.rpcError()
		}
	}
	if err != nil {
		return contact.Record{}, fmt.Errorf("a2a: decode response: %w", err)
	}
	record := result.record(request.Agent)
	if record.ID == "" {
		return contact.Record{}, fmt.Errorf("a2a: response contains neither a task nor a message id")
	}
	return record, nil
}

type rpcResult struct {
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Result struct {
		Task struct {
			ID     string `json:"id"`
			Status struct {
				State string `json:"state"`
			} `json:"status"`
		} `json:"task"`
		Message struct {
			ID string `json:"messageId"`
		} `json:"message"`
		StatusUpdate struct {
			TaskID string `json:"taskId"`
			Status struct {
				State string `json:"state"`
			} `json:"status"`
		} `json:"statusUpdate"`
	} `json:"result"`
}

func (r rpcResult) rpcError() error {
	if r.Error != nil {
		return fmt.Errorf("JSON-RPC %d: %s", r.Error.Code, r.Error.Message)
	}
	return nil
}

func (r rpcResult) record(agent string) contact.Record {
	id, state := r.Result.Task.ID, r.Result.Task.Status.State
	if r.Result.StatusUpdate.TaskID != "" {
		id, state = r.Result.StatusUpdate.TaskID, r.Result.StatusUpdate.Status.State
	}
	if id == "" {
		id, state = r.Result.Message.ID, "message"
	}
	if state == "" {
		state = "unknown"
	}
	return contact.Record{Agent: agent, Kind: "a2a", ID: id, State: state}
}

func decodeStream(reader io.Reader) (rpcResult, error) {
	scanner := bufio.NewScanner(reader)
	var last rpcResult
	found := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event rpcResult
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
			return rpcResult{}, err
		}
		if err := event.rpcError(); err != nil {
			return rpcResult{}, err
		}
		last = event
		found = true
	}
	if err := scanner.Err(); err != nil {
		return rpcResult{}, err
	}
	if !found {
		return rpcResult{}, fmt.Errorf("empty event stream")
	}
	return last, nil
}

func newID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
