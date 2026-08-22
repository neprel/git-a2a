package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/manifest"
)

func TestWhoRoutesChangeToSpecAgent(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, ".git-a2a", "cache", "acme-lib-utils")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join("..", "..", "spec", "examples", "acme-lib-utils.a2amodule.yml")
	b, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(cacheDir, "a2amodule.yml"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"who", "acme-lib-utils", "--intent", "change"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "acme-pm (spec)") || !strings.Contains(out.String(), "github-issue acme/lib-utils") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func TestCheckAgentsUpChangedDownOffline(t *testing.T) {
	card := []byte(`{"name":"owner","description":"test","version":"1","supportedInterfaces":[{"url":"http://example.com","protocolBinding":"JSONRPC","protocolVersion":"1.0"}]}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(card) }))
	m := &manifest.Manifest{Agents: []manifest.Agent{{Name: "owner", Role: "owner", Card: server.URL}}}
	sum := sha256.Sum256(card)
	expected := map[string]string{"owner": "sha256:" + hex.EncodeToString(sum[:])}
	state, failed, _ := checkAgents(m, expected, "", false)
	if failed || state != "1 up" {
		t.Fatalf("up: %s %v", state, failed)
	}
	state, failed, _ = checkAgents(m, map[string]string{"owner": "sha256:" + strings.Repeat("0", 64)}, "", false)
	if !failed || state != "1 changed" {
		t.Fatalf("changed: %s %v", state, failed)
	}
	state, failed, _ = checkAgents(m, expected, "", true)
	if failed || state != "unknown" {
		t.Fatalf("offline: %s %v", state, failed)
	}
	server.Close()
	state, failed, _ = checkAgents(m, expected, "", false)
	if !failed || state != "1 down" {
		t.Fatalf("down: %s %v", state, failed)
	}
}
