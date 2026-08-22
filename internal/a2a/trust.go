package a2a

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

const maxJWKSSize = 2 << 20

type VerifyOptions struct {
	CacheRoot string
	Client    *http.Client
	Offline   bool
}

type Verification struct {
	KeyID     string
	Algorithm string
	JWKS      string
}

// VerifySignatures verifies at least one detached JWS signature over the RFC
// 8785 canonical Agent Card payload. The signatures property is not signed.
func VerifySignatures(raw []byte, options VerifyOptions) (Verification, error) {
	card, err := decodeCard(raw)
	if err != nil {
		return Verification{}, err
	}
	if err := Validate(card); err != nil {
		return Verification{}, err
	}
	rawSignatures, ok := card["signatures"].([]any)
	if !ok || len(rawSignatures) == 0 {
		return Verification{}, fmt.Errorf("card is unsigned")
	}
	payload, err := CanonicalPayload(card)
	if err != nil {
		return Verification{}, fmt.Errorf("canonicalize card: %w", err)
	}
	payload64 := base64.RawURLEncoding.EncodeToString(payload)
	var failures []string
	for i, value := range rawSignatures {
		signature, ok := value.(map[string]any)
		if !ok {
			failures = append(failures, fmt.Sprintf("signature %d is not an object", i+1))
			continue
		}
		verification, verifyErr := verifySignature(signature, payload64, options)
		if verifyErr == nil {
			return verification, nil
		}
		failures = append(failures, fmt.Sprintf("signature %d: %v", i+1, verifyErr))
	}
	return Verification{}, fmt.Errorf("no signature verified: %s", strings.Join(failures, "; "))
}

func verifySignature(signature map[string]any, payload64 string, options VerifyOptions) (Verification, error) {
	protected64, _ := signature["protected"].(string)
	signature64, _ := signature["signature"].(string)
	if protected64 == "" || signature64 == "" {
		return Verification{}, fmt.Errorf("protected and signature are required")
	}
	protectedRaw, err := base64.RawURLEncoding.DecodeString(protected64)
	if err != nil {
		return Verification{}, fmt.Errorf("protected header: %w", err)
	}
	var protected map[string]any
	if err = json.Unmarshal(protectedRaw, &protected); err != nil {
		return Verification{}, fmt.Errorf("protected header: %w", err)
	}
	algorithm, _ := protected["alg"].(string)
	keyID, _ := protected["kid"].(string)
	jwksURL, _ := protected["jku"].(string)
	if algorithm == "" || keyID == "" || jwksURL == "" {
		return Verification{}, fmt.Errorf("protected header requires alg, kid, and jku")
	}
	if typ, ok := protected["typ"].(string); ok && !strings.EqualFold(typ, "JOSE") {
		return Verification{}, fmt.Errorf("protected typ must be JOSE")
	}
	keySet, err := readJWKS(jwksURL, options)
	if err != nil {
		return Verification{}, err
	}
	key, err := selectJWK(keySet, keyID, algorithm)
	if err != nil {
		return Verification{}, err
	}
	signatureBytes, err := base64.RawURLEncoding.DecodeString(signature64)
	if err != nil {
		return Verification{}, fmt.Errorf("signature encoding: %w", err)
	}
	input := []byte(protected64 + "." + payload64)
	if err = verifyJWS(algorithm, key, input, signatureBytes); err != nil {
		return Verification{}, err
	}
	return Verification{KeyID: keyID, Algorithm: algorithm, JWKS: jwksURL}, nil
}

func readJWKS(location string, options VerifyOptions) (map[string]any, error) {
	parsed, err := url.Parse(location)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return nil, fmt.Errorf("jku must be an HTTP(S) URL")
	}
	if parsed.Scheme == "http" && parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "::1" {
		return nil, fmt.Errorf("jku must use HTTPS except on loopback")
	}
	cachePath := ""
	if options.CacheRoot != "" {
		sum := sha256.Sum256([]byte(location))
		cachePath = filepath.Join(options.CacheRoot, ".git-a2a", "jwks", hex.EncodeToString(sum[:])+".json")
	}
	var raw []byte
	if !options.Offline {
		client := options.Client
		if client == nil {
			client = &http.Client{Timeout: 3 * time.Second}
		}
		response, requestErr := client.Get(location)
		if requestErr == nil {
			defer response.Body.Close()
			if response.StatusCode == http.StatusOK {
				raw, requestErr = io.ReadAll(io.LimitReader(response.Body, maxJWKSSize+1))
				if requestErr == nil && len(raw) > maxJWKSSize {
					requestErr = fmt.Errorf("JWKS exceeds %d bytes", maxJWKSSize)
				}
			} else {
				requestErr = fmt.Errorf("HTTP %d", response.StatusCode)
			}
		}
		if requestErr == nil {
			if _, parseErr := parseJWKS(raw); parseErr != nil {
				return nil, parseErr
			}
			if cachePath != "" {
				if cacheErr := writeCache(cachePath, raw); cacheErr != nil {
					return nil, fmt.Errorf("cache JWKS: %w", cacheErr)
				}
			}
			return parseJWKS(raw)
		}
	}
	if cachePath == "" {
		return nil, fmt.Errorf("JWKS unavailable and no cache configured")
	}
	raw, err = os.ReadFile(cachePath)
	if err != nil {
		if options.Offline {
			return nil, fmt.Errorf("JWKS is not cached")
		}
		return nil, fmt.Errorf("JWKS fetch failed and no cache is available")
	}
	return parseJWKS(raw)
}

func parseJWKS(raw []byte) (map[string]any, error) {
	var set map[string]any
	if err := json.Unmarshal(raw, &set); err != nil {
		return nil, fmt.Errorf("invalid JWKS: %w", err)
	}
	if _, ok := set["keys"].([]any); !ok {
		return nil, fmt.Errorf("invalid JWKS: keys must be an array")
	}
	return set, nil
}

func writeCache(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".jwks-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0o644); err == nil {
		_, err = temporary.Write(raw)
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func selectJWK(set map[string]any, keyID, algorithm string) (map[string]any, error) {
	for _, raw := range set["keys"].([]any) {
		key, ok := raw.(map[string]any)
		if !ok || key["kid"] != keyID {
			continue
		}
		if value, _ := key["alg"].(string); value != "" && value != algorithm {
			continue
		}
		if value, _ := key["use"].(string); value != "" && value != "sig" {
			continue
		}
		return key, nil
	}
	return nil, fmt.Errorf("JWKS has no signing key %q for %s", keyID, algorithm)
}

func verifyJWS(algorithm string, jwk map[string]any, input, signature []byte) error {
	hash := sha256.Sum256(input)
	switch algorithm {
	case "ES256":
		key, err := ecPublicKey(jwk)
		if err != nil {
			return err
		}
		if len(signature) != 64 || !ecdsa.Verify(key, hash[:], new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])) {
			return fmt.Errorf("ES256 signature is invalid")
		}
	case "RS256":
		key, err := rsaPublicKey(jwk)
		if err != nil {
			return err
		}
		if err = rsa.VerifyPKCS1v15(key, crypto.SHA256, hash[:], signature); err != nil {
			return fmt.Errorf("RS256 signature is invalid")
		}
	case "EdDSA":
		key, err := edPublicKey(jwk)
		if err != nil {
			return err
		}
		if !ed25519.Verify(key, input, signature) {
			return fmt.Errorf("EdDSA signature is invalid")
		}
	default:
		return fmt.Errorf("unsupported JWS algorithm %q", algorithm)
	}
	return nil
}

func ecPublicKey(jwk map[string]any) (*ecdsa.PublicKey, error) {
	if jwk["kty"] != "EC" || jwk["crv"] != "P-256" {
		return nil, fmt.Errorf("ES256 requires an EC P-256 key")
	}
	x, err := decodeBigInt(jwk, "x")
	if err != nil {
		return nil, err
	}
	y, err := decodeBigInt(jwk, "y")
	if err != nil {
		return nil, err
	}
	key := &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
	if !key.Curve.IsOnCurve(x, y) {
		return nil, fmt.Errorf("EC key is not on P-256")
	}
	return key, nil
}

func rsaPublicKey(jwk map[string]any) (*rsa.PublicKey, error) {
	if jwk["kty"] != "RSA" {
		return nil, fmt.Errorf("RS256 requires an RSA key")
	}
	n, err := decodeBigInt(jwk, "n")
	if err != nil {
		return nil, err
	}
	eBytes, err := decodeBase64Field(jwk, "e")
	if err != nil {
		return nil, err
	}
	e := new(big.Int).SetBytes(eBytes)
	if !e.IsInt64() || e.Sign() <= 0 {
		return nil, fmt.Errorf("invalid RSA exponent")
	}
	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}

func edPublicKey(jwk map[string]any) (ed25519.PublicKey, error) {
	if jwk["kty"] != "OKP" || jwk["crv"] != "Ed25519" {
		return nil, fmt.Errorf("EdDSA requires an OKP Ed25519 key")
	}
	x, err := decodeBase64Field(jwk, "x")
	if err != nil {
		return nil, err
	}
	if len(x) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid Ed25519 key size")
	}
	return ed25519.PublicKey(x), nil
}

func decodeBigInt(jwk map[string]any, field string) (*big.Int, error) {
	raw, err := decodeBase64Field(jwk, field)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(raw), nil
}

func decodeBase64Field(jwk map[string]any, field string) ([]byte, error) {
	value, _ := jwk[field].(string)
	if value == "" {
		return nil, fmt.Errorf("JWK field %s is required", field)
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("JWK field %s: %w", field, err)
	}
	return raw, nil
}

// CanonicalPayload returns the RFC 8785 JSON representation signed by A2A.
func CanonicalPayload(card map[string]any) ([]byte, error) {
	copy := make(map[string]any, len(card)-1)
	for key, value := range card {
		if key != "signatures" {
			copy[key] = cloneJSON(value)
		}
	}
	pruneCardDefaults(copy)
	var out bytes.Buffer
	if err := appendCanonical(&out, copy); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func cloneJSON(value any) any {
	switch value := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			result[key] = cloneJSON(item)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for i, item := range value {
			result[i] = cloneJSON(item)
		}
		return result
	default:
		return value
	}
}

func pruneCardDefaults(card map[string]any) {
	deleteEmptyArray(card, "securityRequirements")
	if capabilities, ok := card["capabilities"].(map[string]any); ok {
		deleteEmptyArray(capabilities, "extensions")
	}
	if skills, ok := card["skills"].([]any); ok {
		for _, raw := range skills {
			if skill, ok := raw.(map[string]any); ok {
				for _, key := range []string{"examples", "inputModes", "outputModes", "securityRequirements"} {
					deleteEmptyArray(skill, key)
				}
			}
		}
	}
}

func deleteEmptyArray(object map[string]any, key string) {
	if values, ok := object[key].([]any); ok && len(values) == 0 {
		delete(object, key)
	}
}

func appendCanonical(out *bytes.Buffer, value any) error {
	switch value := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		out.WriteString(strconv.FormatBool(value))
	case string:
		return appendJSONString(out, value)
	case json.Number:
		return appendJSONNumber(out, value.String())
	case float64:
		return appendJSONFloat(out, value)
	case []any:
		out.WriteByte('[')
		for i, item := range value {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := appendCanonical(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool { return utf16Less(keys[i], keys[j]) })
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := appendJSONString(out, key); err != nil {
				return err
			}
			out.WriteByte(':')
			if err := appendCanonical(out, value[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return fmt.Errorf("unsupported JSON value %T", value)
	}
	return nil
}

func appendJSONString(out *bytes.Buffer, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("invalid UTF-8 string")
	}
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\b':
			out.WriteString(`\b`)
		case '\t':
			out.WriteString(`\t`)
		case '\n':
			out.WriteString(`\n`)
		case '\f':
			out.WriteString(`\f`)
		case '\r':
			out.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(out, `\u%04x`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
	return nil
}

func appendJSONNumber(out *bytes.Buffer, value string) error {
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fmt.Errorf("number %q is not an IEEE 754 value", value)
	}
	return appendJSONFloat(out, number)
}

func appendJSONFloat(out *bytes.Buffer, value float64) error {
	if value == 0 {
		out.WriteByte('0')
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	out.Write(raw)
	return nil
}

func utf16Less(left, right string) bool {
	a := utf16.Encode([]rune(left))
	b := utf16.Encode([]rune(right))
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}
