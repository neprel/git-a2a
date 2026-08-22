package a2a

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
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
	verification, err := VerifySignatures(raw, VerifyOptions{CacheRoot: cacheRoot})
	if err != nil {
		t.Fatal(err)
	}
	if verification.KeyID != "key-1" || verification.Algorithm != "ES256" || requests.Load() != 1 {
		t.Fatalf("verification=%+v requests=%d", verification, requests.Load())
	}
	server.Close()
	if _, err = VerifySignatures(raw, VerifyOptions{CacheRoot: cacheRoot, Offline: true}); err != nil {
		t.Fatalf("cached offline verification: %v", err)
	}

	card["description"] = "tampered"
	tampered, _ := json.Marshal(card)
	if _, err = VerifySignatures(tampered, VerifyOptions{CacheRoot: cacheRoot, Offline: true}); err == nil {
		t.Fatal("tampered card verified")
	}
	delete(card, "signatures")
	unsigned, _ := json.Marshal(card)
	if _, err = VerifySignatures(unsigned, VerifyOptions{CacheRoot: cacheRoot, Offline: true}); err == nil {
		t.Fatal("unsigned card verified")
	}
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
