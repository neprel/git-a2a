package a2a

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCanonicalPayloadRFC8785NumbersAndKeyOrder(t *testing.T) {
	card := map[string]any{
		"numbers": []any{json.Number("333333333.33333329"), json.Number("1E30"), json.Number("4.50"), json.Number("2e-3"), json.Number("1e-27")},
		"€":       "Euro Sign",
		"\r":      "Carriage Return",
		"1":       "One",
		"😀":       "Emoji",
		"\u0080":  "Control",
		"ö":       "Latin Small Letter O With Diaeresis",
		"signatures": []any{map[string]any{
			"protected": "excluded",
		}},
	}
	got, err := CanonicalPayload(card)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"\\r\":\"Carriage Return\",\"1\":\"One\",\"numbers\":[333333333.3333333,1e+30,4.5,0.002,1e-27],\"\u0080\":\"Control\",\"ö\":\"Latin Small Letter O With Diaeresis\",\"€\":\"Euro Sign\",\"😀\":\"Emoji\"}"
	if string(got) != want {
		t.Fatalf("canonical JSON:\n got %s\nwant %s", got, want)
	}
}

func TestJWKThumbprintRFC7638CanonicalMembers(t *testing.T) {
	jwk := map[string]any{
		"kty": "OKP", "crv": "Ed25519", "x": "11qYAYdk9Jz9Lq6j5b-p8p3lGFnz4mVf7zK9G2mV8pI",
		"kid": "ignored", "alg": "EdDSA", "use": "sig",
	}
	got, err := JWKThumbprint(jwk)
	if err != nil {
		t.Fatal(err)
	}
	canonical := []byte(`{"crv":"Ed25519","kty":"OKP","x":"11qYAYdk9Jz9Lq6j5b-p8p3lGFnz4mVf7zK9G2mV8pI"}`)
	sum := sha256.Sum256(canonical)
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("thumbprint = %s, want %s", got, want)
	}
	if strings.Contains(got, "=") {
		t.Fatalf("thumbprint is padded: %s", got)
	}
}

func TestReadJWKSSuppressesHTMLResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, "<html><p>secret</p></html>")
	}))
	defer server.Close()

	_, _, err := readJWKS(server.URL, VerifyOptions{})
	if err == nil {
		t.Fatal("readJWKS unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), "<") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("HTML leaked into error: %q", err)
	}
}

func TestVerifySignaturesRequiresPinnedOrSameOriginKeySource(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{ecJWK("key-1", &privateKey.PublicKey)}})
	}))
	defer server.Close()
	card := signedTestCard(t, privateKey, server.URL+"/jwks", "key-1")
	raw, _ := json.Marshal(card)
	jwk := ecJWK("key-1", &privateKey.PublicKey)
	thumbprint, _ := JWKThumbprint(jwk)

	if _, err = VerifySignatures(raw, VerifyOptions{CardURL: "https://foreign.example/card"}); err == nil || !strings.Contains(err.Error(), "same origin") {
		t.Fatalf("foreign unpinned jku error = %v", err)
	}
	if _, err = VerifySignatures(raw, VerifyOptions{CardURL: "https://foreign.example/card", PinnedJWKS: []string{server.URL + "/jwks"}}); err != nil {
		t.Fatalf("pinned JWKS: %v", err)
	}
	if _, err = VerifySignatures(raw, VerifyOptions{CardURL: "https://foreign.example/card", PinnedKeys: []string{thumbprint}}); err != nil {
		t.Fatalf("pinned thumbprint: %v", err)
	}
	if _, err = VerifySignatures(raw, VerifyOptions{CardURL: "https://foreign.example/card", PinnedKeys: []string{"wrong"}}); err == nil || !strings.Contains(err.Error(), "not pinned") {
		t.Fatalf("wrong pin error = %v", err)
	}
}

func TestFreshJWKSRevokesMissingKey(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	revoked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		keys := []any{ecJWK("key-1", &privateKey.PublicKey)}
		if revoked {
			keys = []any{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	}))
	defer server.Close()
	card := signedTestCard(t, privateKey, server.URL+"/jwks", "key-1")
	raw, _ := json.Marshal(card)
	root := t.TempDir()
	options := VerifyOptions{CacheRoot: root, CardURL: server.URL + "/card", MaxAge: time.Hour}
	if _, err = VerifySignatures(raw, options); err != nil {
		t.Fatal(err)
	}
	revoked = true
	sum := sha256.Sum256([]byte(server.URL + "/jwks"))
	cachePath := filepath.Join(root, ".git-a2a", "jwks", fmt.Sprintf("%x.json", sum))
	old := time.Now().Add(-2 * time.Hour)
	if err = os.Chtimes(cachePath, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifySignatures(raw, options); err == nil || !strings.Contains(err.Error(), "no signing key") {
		t.Fatalf("revoked key error = %v", err)
	}
	if body, readErr := os.ReadFile(cachePath); readErr != nil || bytes.Contains(body, []byte("key-1")) {
		t.Fatalf("fresh cache retained revoked key: %v %s", readErr, body)
	}
}

func TestCanonicalPayloadOmitsEmptyOptionalRepeatedFields(t *testing.T) {
	card := map[string]any{
		"capabilities":         map[string]any{"streaming": false, "extensions": []any{}},
		"description":          "",
		"name":                 "Example Agent",
		"securityRequirements": []any{},
		"skills":               []any{},
	}
	got, err := CanonicalPayload(card)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"capabilities":{"streaming":false},"description":"","name":"Example Agent","skills":[]}`
	if string(got) != want {
		t.Fatalf("canonical JSON:\n got %s\nwant %s", got, want)
	}
}

func TestVerifySignaturesWithGeneratedKeyAndCachedJWKS(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{ecJWK("key-1", &privateKey.PublicKey)}})
	}))
	card := signedTestCard(t, privateKey, server.URL, "key-1")
	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatal(err)
	}
	cacheRoot := t.TempDir()
	verification, err := VerifySignatures(raw, VerifyOptions{CacheRoot: cacheRoot, CardURL: server.URL + "/card"})
	if err != nil {
		t.Fatal(err)
	}
	if verification.KeyID != "key-1" || verification.Algorithm != "ES256" || requests.Load() != 1 {
		t.Fatalf("verification=%+v requests=%d", verification, requests.Load())
	}
	server.Close()
	if _, err = VerifySignatures(raw, VerifyOptions{CacheRoot: cacheRoot, Offline: true, CardURL: server.URL + "/card"}); err != nil {
		t.Fatalf("cached offline verification: %v", err)
	}

	card["description"] = "tampered"
	tampered, _ := json.Marshal(card)
	if _, err = VerifySignatures(tampered, VerifyOptions{CacheRoot: cacheRoot, Offline: true, CardURL: server.URL + "/card"}); err == nil {
		t.Fatal("tampered card verified")
	}
	delete(card, "signatures")
	unsigned, _ := json.Marshal(card)
	if _, err = VerifySignatures(unsigned, VerifyOptions{CacheRoot: cacheRoot, Offline: true, CardURL: server.URL + "/card"}); err == nil {
		t.Fatal("unsigned card verified")
	}
}

func TestPublishedDemoCardsVerifyAgainstPublishedJWKS(t *testing.T) {
	site := filepath.Join("..", "..", "sites", "git-a2a.com")
	jwks, err := os.ReadFile(filepath.Join(site, "demo", "agents", ".well-known", "jwks.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://git-a2a.com/demo/agents/.well-known/jwks.json" {
			return nil, fmt.Errorf("unexpected request %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(jwks)), Header: make(http.Header)}, nil
	})}
	for _, agent := range []string{"acme-lib-utils", "acme-pm"} {
		raw, readErr := os.ReadFile(filepath.Join(site, "demo", "agents", agent, ".well-known", "agent-card.json"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		verification, verifyErr := VerifySignatures(raw, VerifyOptions{
			Client:     client,
			CardURL:    "https://git-a2a.com/demo/agents/" + agent + "/.well-known/agent-card.json",
			PinnedJWKS: []string{"https://git-a2a.com/demo/agents/.well-known/jwks.json"},
		})
		if verifyErr != nil {
			t.Fatalf("%s: %v", agent, verifyErr)
		}
		if verification.KeyID != "acme-demo-2026" || verification.Algorithm != "EdDSA" {
			t.Fatalf("%s verification = %+v", agent, verification)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func signedTestCard(t *testing.T, privateKey *ecdsa.PrivateKey, jwksURL, keyID string) map[string]any {
	t.Helper()
	card := map[string]any{
		"name": "signed-agent", "description": "Signed test agent.", "version": "1.0.0",
		"supportedInterfaces": []any{map[string]any{"url": "https://agent.example/a2a", "protocolBinding": "JSONRPC", "protocolVersion": "1.0"}},
		"capabilities":        map[string]any{},
		"defaultInputModes":   []any{"text/plain"},
		"defaultOutputModes":  []any{"text/plain"},
		"skills":              []any{},
	}
	headerRaw, _ := json.Marshal(map[string]any{"alg": "ES256", "typ": "JOSE", "kid": keyID, "jku": jwksURL})
	protected := base64.RawURLEncoding.EncodeToString(headerRaw)
	payload, err := CanonicalPayload(card)
	if err != nil {
		t.Fatal(err)
	}
	input := []byte(protected + "." + base64.RawURLEncoding.EncodeToString(payload))
	digest := sha256.Sum256(input)
	r, s, err := ecdsa.Sign(rand.Reader, privateKey, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	card["signatures"] = []any{map[string]any{"protected": protected, "signature": base64.RawURLEncoding.EncodeToString(signature)}}
	return card
}

func ecJWK(keyID string, key *ecdsa.PublicKey) map[string]any {
	size := (key.Curve.Params().BitSize + 7) / 8
	return map[string]any{
		"kty": "EC", "crv": "P-256", "kid": keyID, "use": "sig", "alg": "ES256",
		"x": base64.RawURLEncoding.EncodeToString(key.X.FillBytes(make([]byte, size))),
		"y": base64.RawURLEncoding.EncodeToString(key.Y.FillBytes(make([]byte, size))),
	}
}
