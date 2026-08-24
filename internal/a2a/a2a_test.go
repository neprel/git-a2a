package a2a

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/manifest"
)

func TestReadSuppressesHTMLResponseBody(t *testing.T) {
	body := "<!doctype html><p>secret</p>"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, body)
	}))
	defer server.Close()

	_, _, err := Read(server.URL, "")
	want := fmt.Sprintf("%s: HTTP 403 Forbidden (html response, %d bytes, suppressed)", server.URL, len(body))
	if err == nil || err.Error() != want {
		t.Fatalf("Read() error = %q, want %q", err, want)
	}
	if strings.Contains(err.Error(), "<") {
		t.Fatalf("HTML leaked into error: %q", err)
	}
}

func TestReadV1AndLegacy(t *testing.T) {
	for _, name := range []string{"v1.json", "v0.3.json"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "cards", name))
		if err != nil {
			t.Fatal(err)
		}
		card, err := Parse(raw)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		interfaces, ok := card["supportedInterfaces"].([]any)
		if !ok || len(interfaces) == 0 {
			t.Fatalf("%s not normalized", name)
		}
	}
}

func TestSnapshotURLAndRelativeCard(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "cards", "v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(raw) }))
	defer server.Close()
	m := &manifest.Manifest{Agents: []manifest.Agent{{Name: "acme-lib-utils", Role: "owner", Card: server.URL}, {Name: "legacy-agent", Role: "owner", Card: "cards/legacy.json"}}}
	legacy, err := os.ReadFile(filepath.Join("..", "..", "testdata", "cards", "v0.3.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	hashes, warnings := Snapshot(m, dir, func(path string) ([]byte, error) {
		if path != "cards/legacy.json" {
			t.Fatalf("path %s", path)
		}
		return legacy, nil
	})
	if len(warnings) != 0 || len(hashes) != 2 {
		t.Fatalf("hashes=%v warnings=%v", hashes, warnings)
	}
	for _, name := range []string{"acme-lib-utils", "legacy-agent"} {
		if _, err := os.Stat(filepath.Join(dir, FileName(name))); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExportAddsOneModuleExtensionAndPreservesSkills(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "cards", "v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	base, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	agent := manifest.Agent{Name: "acme-lib-utils", Role: "owner", Scope: []string{"src/**"}}
	card, err := Export(base, Binding{Module: "acme-lib-utils", Repository: "https://github.com/acme/lib-utils.git", Ref: "main", Agent: agent})
	if err != nil {
		t.Fatal(err)
	}
	skills := card["skills"].([]any)
	if len(skills) != 1 {
		t.Fatal("skills changed")
	}
	caps := card["capabilities"].(map[string]any)
	extensions := caps["extensions"].([]any)
	if len(extensions) != 1 {
		t.Fatalf("extensions: %#v", extensions)
	}
	extension := extensions[0].(map[string]any)
	if extension["uri"] != ExtensionURI || extension["required"] != false {
		t.Fatalf("extension: %#v", extension)
	}
	params := extension["params"].(map[string]any)
	if params["module"] != "acme-lib-utils" || params["role"] != "owner" {
		t.Fatalf("params: %#v", params)
	}
	encoded, err := Marshal(card)
	if err != nil {
		t.Fatal(err)
	}
	var round map[string]any
	if json.Unmarshal(encoded, &round) != nil {
		t.Fatal("invalid exported JSON")
	}
}

func TestSynthesizeRequiresA2AContact(t *testing.T) {
	agent := manifest.Agent{Name: "owner", Role: "owner", Contacts: []manifest.Contact{{Intents: []string{"question"}, Kind: "a2a", URL: "https://agent.example/a2a"}}}
	card, err := Export(nil, Binding{Module: "demo", Agent: agent, ModuleDescription: "Demo owner."})
	if err != nil {
		t.Fatal(err)
	}
	if err = Validate(card); err != nil {
		t.Fatal(err)
	}
	_, err = Export(nil, Binding{Module: "demo", Agent: manifest.Agent{Name: "no-interface", Role: "owner"}})
	if err == nil {
		t.Fatal("export without interface succeeded")
	}
}

func TestExportUpgradesLegacyProtocolVersion(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "cards", "v0.3.json"))
	if err != nil {
		t.Fatal(err)
	}
	base, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	card, err := Export(base, Binding{Module: "demo", Agent: manifest.Agent{Name: "legacy-agent", Role: "owner"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, rawInterface := range card["supportedInterfaces"].([]any) {
		if got := rawInterface.(map[string]any)["protocolVersion"]; got != "1.0" {
			t.Fatalf("protocolVersion = %#v, want 1.0", got)
		}
	}
}
