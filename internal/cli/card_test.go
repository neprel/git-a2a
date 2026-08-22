package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/a2a"
	"github.com/neprel/git-a2a/internal/manifest"
)

func TestCardExportSynthesizesValidV1Card(t *testing.T) {
	root := t.TempDir()
	manifestBytes := []byte("schema: 1\nmodule:\n  id: demo\n  description: Demo module owner.\n  repository: https://github.com/acme/demo.git\nagents:\n  - name: demo-owner\n    role: owner\n    contacts:\n      - intents: [question]\n        kind: a2a\n        url: https://agents.acme.example/demo\n")
	if err := os.WriteFile(filepath.Join(root, "a2amodule.yml"), manifestBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	app.Runner = scriptedGitRunner{output: []byte("ref: refs/heads/trunk\tHEAD\n1111111111111111111111111111111111111111\tHEAD\n")}
	if code := app.Run([]string{"card", "export", "demo-owner"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var card map[string]any
	if json.Unmarshal(out.Bytes(), &card) != nil {
		t.Fatal("invalid json")
	}
	if err := a2a.Validate(card); err != nil {
		t.Fatal(err)
	}
	caps := card["capabilities"].(map[string]any)
	extensions := caps["extensions"].([]any)
	if extensions[0].(map[string]any)["uri"] != a2a.ExtensionURI {
		t.Fatalf("extension: %#v", extensions)
	}
	params := extensions[0].(map[string]any)["params"].(map[string]any)
	if params["ref"] != "trunk" {
		t.Fatalf("exported ref = %#v, want default branch trunk", params["ref"])
	}
}

func TestCheckAgentsUsesRelativeCardSnapshot(t *testing.T) {
	root := t.TempDir()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "cards", "v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, a2a.FileName("acme-lib-utils")), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	m := &manifest.Manifest{Agents: []manifest.Agent{{Name: "acme-lib-utils", Card: "cards/agent.json"}}}
	state, failed, details := checkAgents(m, map[string]string{"acme-lib-utils": "sha256:" + hex.EncodeToString(sum[:])}, root, root, false)
	if failed || state != "1 up" || len(details) != 0 {
		t.Fatalf("state=%q failed=%v details=%v", state, failed, details)
	}
}

func TestRequiredCardSignatureFailsStatusAndWarnsUpdate(t *testing.T) {
	root := t.TempDir()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "cards", "v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	agent := manifest.Agent{Name: "acme-lib-utils", Card: "cards.json", Trust: &manifest.Trust{Signatures: true}}
	m := &manifest.Manifest{Agents: []manifest.Agent{agent}}
	if err = os.WriteFile(filepath.Join(root, "cards.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	state, failed, details := checkAgents(m, nil, root, root, false)
	if !failed || state != "1 untrusted" || !strings.Contains(strings.Join(details, "\n"), "card is unsigned") {
		t.Fatalf("state=%q failed=%v details=%v", state, failed, details)
	}
	cardsDir := filepath.Join(root, "snapshots")
	if err = os.MkdirAll(cardsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(cardsDir, a2a.FileName(agent.Name)), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	warnings := trustedCardWarnings(m, cardsDir, root)
	if len(warnings) != 1 || !strings.Contains(warnings[0].Error(), "card is unsigned") {
		t.Fatalf("warnings=%v", warnings)
	}
}

func TestCardVerifyUsesGeneratedKeyAndJWKS(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var cardRaw []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/jwks" {
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{
				"kty": "OKP", "crv": "Ed25519", "kid": "generated", "use": "sig", "alg": "EdDSA",
				"x": base64.RawURLEncoding.EncodeToString(publicKey),
			}}})
			return
		}
		_, _ = w.Write(cardRaw)
	}))
	defer server.Close()
	card := map[string]any{
		"name": "verified-agent", "description": "Generated test agent.", "version": "1.0.0",
		"supportedInterfaces": []any{map[string]any{"url": server.URL, "protocolBinding": "JSONRPC", "protocolVersion": "1.0"}},
		"capabilities":        map[string]any{}, "defaultInputModes": []any{"text/plain"}, "defaultOutputModes": []any{"text/plain"}, "skills": []any{},
	}
	header, _ := json.Marshal(map[string]any{"alg": "EdDSA", "typ": "JOSE", "kid": "generated", "jku": server.URL + "/jwks"})
	protected := base64.RawURLEncoding.EncodeToString(header)
	payload, err := a2a.CanonicalPayload(card)
	if err != nil {
		t.Fatal(err)
	}
	input := []byte(protected + "." + base64.RawURLEncoding.EncodeToString(payload))
	card["signatures"] = []any{map[string]any{"protected": protected, "signature": base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, input))}}
	cardRaw, _ = json.Marshal(card)

	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = t.TempDir()
	if code := app.Run([]string{"card", "verify", server.URL + "/card"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "verified EdDSA signature with key generated") {
		t.Fatalf("output = %q", out.String())
	}

	absolute := filepath.Join(t.TempDir(), "signed-card.json")
	if err = os.WriteFile(absolute, cardRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	app.Root = t.TempDir()
	if code := app.Run([]string{"card", "verify", absolute}); code != 0 {
		t.Fatalf("absolute path exit %d: %s", code, errOut.String())
	}
}

func TestCardVerifyUnresolvedLocationIsUsageFailure(t *testing.T) {
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing-card.json")
	if code := app.Run([]string{"card", "verify", missing}); code != 2 {
		t.Fatalf("exit %d, want 2: %s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "nothing resolved") || strings.Contains(errOut.String(), "signature invalid") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestCardExportStripsRepositoryURLUserinfo(t *testing.T) {
	for _, test := range []struct {
		name       string
		repository string
		remote     string
	}{
		{name: "manifest repository", repository: "https://user:TOKEN@example.test/acme/lib.git"},
		{name: "git remote", remote: "https://user:TOKEN@example.test/acme/lib.git"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			repositoryLine := ""
			if test.repository != "" {
				repositoryLine = "  repository: " + test.repository + "\n"
			}
			manifest := "schema: 1\nmodule:\n  id: acme-lib\n" + repositoryLine + "agents:\n  - name: owner\n    role: owner\n    contacts:\n      - intents: [question]\n        kind: a2a\n        url: https://agent.example.test/\n"
			if err := os.WriteFile(filepath.Join(root, "a2amodule.yml"), []byte(manifest), 0o644); err != nil {
				t.Fatal(err)
			}
			if test.remote != "" {
				runGitForCard(t, root, "init")
				runGitForCard(t, root, "remote", "add", "origin", test.remote)
			}
			var out, errOut bytes.Buffer
			app := New(&out, &errOut)
			app.Root = root
			if code := app.Run([]string{"card", "export", "owner"}); code != 0 {
				t.Fatalf("exit %d: %s", code, errOut.String())
			}
			if strings.Contains(out.String(), "user") || strings.Contains(out.String(), "TOKEN") || !strings.Contains(out.String(), "https://example.test/acme/lib.git") {
				t.Fatalf("userinfo leaked:\n%s", out.String())
			}
		})
	}
}

func runGitForCard(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

type scriptedGitRunner struct {
	output []byte
	err    error
}

func (r scriptedGitRunner) Run(context.Context, string, []byte, ...string) ([]byte, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.output == nil {
		return nil, errors.New("git operation unavailable in test")
	}
	return r.output, nil
}
