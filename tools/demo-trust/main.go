// Command demo-trust maintains the public demo's Ed25519 JWKS and detached
// Agent Card signatures. The private key path must be outside the repository
// and is never a publication artifact.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"os"

	"github.com/neprel/git-a2a/internal/a2a"
)

const (
	keyID   = "acme-demo-2026"
	jwksURL = "https://git-a2a.com/demo/agents/.well-known/jwks.json"
)

type cardsFlag []string

func (values *cardsFlag) String() string { return fmt.Sprint([]string(*values)) }
func (values *cardsFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func main() {
	privatePath := flag.String("private", "", "private PKCS8 key outside the repository")
	jwksPath := flag.String("jwks", "", "JWKS output path")
	var cards cardsFlag
	flag.Var(&cards, "card", "Agent Card to sign (repeatable)")
	flag.Parse()
	if *privatePath == "" || *jwksPath == "" || len(cards) == 0 {
		flag.Usage()
		os.Exit(2)
	}
	privateKey, err := loadOrCreate(*privatePath)
	must(err)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	must(writeJWKS(*jwksPath, publicKey))
	for _, path := range cards {
		must(signCard(path, privateKey))
	}
}

func loadOrCreate(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		_, privateKey, generateErr := ed25519.GenerateKey(rand.Reader)
		if generateErr != nil {
			return nil, generateErr
		}
		der, marshalErr := x509.MarshalPKCS8PrivateKey(privateKey)
		if marshalErr != nil {
			return nil, marshalErr
		}
		if writeErr := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); writeErr != nil {
			return nil, writeErr
		}
		return privateKey, nil
	}
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("private key is not PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	privateKey, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not Ed25519")
	}
	return privateKey, nil
}

func writeJWKS(path string, publicKey ed25519.PublicKey) error {
	set := map[string]any{"keys": []any{map[string]any{
		"alg": "EdDSA", "crv": "Ed25519", "kid": keyID, "kty": "OKP",
		"use": "sig", "x": base64.RawURLEncoding.EncodeToString(publicKey),
	}}}
	body, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0o644)
}

func signCard(path string, privateKey ed25519.PrivateKey) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var card map[string]any
	if err = json.Unmarshal(raw, &card); err != nil {
		return err
	}
	delete(card, "signatures")
	payload, err := a2a.CanonicalPayload(card)
	if err != nil {
		return err
	}
	header, err := json.Marshal(map[string]any{"alg": "EdDSA", "jku": jwksURL, "kid": keyID, "typ": "JOSE"})
	if err != nil {
		return err
	}
	protected := base64.RawURLEncoding.EncodeToString(header)
	input := protected + "." + base64.RawURLEncoding.EncodeToString(payload)
	card["signatures"] = []any{map[string]any{
		"protected": protected,
		"signature": base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(input))),
	}}
	body, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0o644)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "demo-trust:", err)
		os.Exit(1)
	}
}
