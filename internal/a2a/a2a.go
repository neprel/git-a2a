package a2a

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/neprel/git-a2a/internal/manifest"
)

const ExtensionURI = "https://git-a2a.com/ext/module/v1"

type Card struct {
	Name                 string                `json:"name"`
	Description          string                `json:"description"`
	SupportedInterfaces  []Interface           `json:"supportedInterfaces"`
	Provider             *Provider             `json:"provider,omitempty"`
	Version              string                `json:"version"`
	DocumentationURL     string                `json:"documentationUrl,omitempty"`
	Capabilities         Capabilities          `json:"capabilities"`
	SecuritySchemes      map[string]any        `json:"securitySchemes,omitempty"`
	SecurityRequirements []SecurityRequirement `json:"securityRequirements,omitempty"`
	DefaultInputModes    []string              `json:"defaultInputModes"`
	DefaultOutputModes   []string              `json:"defaultOutputModes"`
	Skills               []Skill               `json:"skills"`
	Signatures           []Signature           `json:"signatures,omitempty"`
	IconURL              string                `json:"iconUrl,omitempty"`
}
type Provider struct {
	URL          string `json:"url"`
	Organization string `json:"organization"`
}
type Interface struct {
	URL             string `json:"url"`
	ProtocolBinding string `json:"protocolBinding"`
	Tenant          string `json:"tenant,omitempty"`
	ProtocolVersion string `json:"protocolVersion"`
}
type Capabilities struct {
	Streaming         *bool       `json:"streaming,omitempty"`
	PushNotifications *bool       `json:"pushNotifications,omitempty"`
	Extensions        []Extension `json:"extensions,omitempty"`
	ExtendedAgentCard *bool       `json:"extendedAgentCard,omitempty"`
}
type Extension struct {
	URI         string         `json:"uri"`
	Description string         `json:"description,omitempty"`
	Required    bool           `json:"required"`
	Params      map[string]any `json:"params,omitempty"`
}
type Skill struct {
	ID                   string                `json:"id"`
	Name                 string                `json:"name"`
	Description          string                `json:"description"`
	Tags                 []string              `json:"tags"`
	Examples             []string              `json:"examples,omitempty"`
	InputModes           []string              `json:"inputModes,omitempty"`
	OutputModes          []string              `json:"outputModes,omitempty"`
	SecurityRequirements []SecurityRequirement `json:"securityRequirements,omitempty"`
}
type SecurityRequirement struct {
	Schemes map[string]StringList `json:"schemes"`
}
type StringList struct {
	List []string `json:"list"`
}
type Signature struct {
	Protected string         `json:"protected"`
	Signature string         `json:"signature"`
	Header    map[string]any `json:"header,omitempty"`
}

// LocationError reports that a card location could not be retrieved. It is
// distinct from errors returned while parsing or validating retrieved bytes.
type LocationError struct {
	Location string
	Err      error
}

func (e *LocationError) Error() string { return e.Location + ": " + e.Err.Error() }
func (e *LocationError) Unwrap() error { return e.Err }

func Read(location, base string) (map[string]any, []byte, error) {
	var b []byte
	var err error
	if strings.HasPrefix(location, "http://") || strings.HasPrefix(location, "https://") {
		client := &http.Client{Timeout: 3 * time.Second}
		resp, e := client.Get(location)
		if e != nil {
			return nil, nil, &LocationError{Location: location, Err: e}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, nil, &LocationError{Location: location, Err: fmt.Errorf("HTTP %d", resp.StatusCode)}
		}
		b, err = io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	} else {
		path := filepath.FromSlash(location)
		if !filepath.IsAbs(path) {
			path = filepath.Join(base, path)
		}
		b, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, nil, &LocationError{Location: location, Err: err}
	}
	card, err := Parse(b)
	return card, b, err
}
func Parse(b []byte) (map[string]any, error) {
	card, err := decodeCard(b)
	if err != nil {
		return nil, err
	}
	normalizeLegacy(card)
	if err := Validate(card); err != nil {
		return nil, err
	}
	return card, nil
}

func decodeCard(b []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var card map[string]any
	if err := dec.Decode(&card); err != nil {
		return nil, err
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return nil, fmt.Errorf("card contains trailing JSON")
	}
	return card, nil
}

func normalizeLegacy(card map[string]any) {
	if _, ok := card["supportedInterfaces"]; ok {
		return
	}
	rawURL, _ := card["url"].(string)
	if rawURL == "" {
		return
	}
	binding, _ := card["preferredTransport"].(string)
	if binding == "" {
		binding = "JSONRPC"
	}
	version, _ := card["protocolVersion"].(string)
	if version == "" {
		version = "0.3"
	}
	interfaces := []any{map[string]any{"url": rawURL, "protocolBinding": strings.ToUpper(binding), "protocolVersion": version}}
	if extra, ok := card["additionalInterfaces"].([]any); ok {
		interfaces = append(interfaces, extra...)
	}
	card["supportedInterfaces"] = interfaces
	delete(card, "url")
	delete(card, "preferredTransport")
	delete(card, "protocolVersion")
	delete(card, "additionalInterfaces")
}

func Validate(card map[string]any) error {
	var errs []error
	requiredString := func(key string) {
		if v, ok := card[key].(string); !ok || strings.TrimSpace(v) == "" {
			errs = append(errs, fmt.Errorf("%s: required non-empty string", key))
		}
	}
	requiredString("name")
	requiredString("description")
	requiredString("version")
	interfaces, ok := card["supportedInterfaces"].([]any)
	if !ok || len(interfaces) == 0 {
		errs = append(errs, fmt.Errorf("supportedInterfaces: at least one interface is required"))
	} else {
		for i, raw := range interfaces {
			p := fmt.Sprintf("supportedInterfaces[%d]", i)
			item, ok := raw.(map[string]any)
			if !ok {
				errs = append(errs, fmt.Errorf("%s: must be an object", p))
				continue
			}
			for _, key := range []string{"url", "protocolBinding", "protocolVersion"} {
				v, ok := item[key].(string)
				if !ok || v == "" {
					errs = append(errs, fmt.Errorf("%s.%s: required", p, key))
					continue
				}
				if key == "url" {
					u, e := url.Parse(v)
					if e != nil || u.Scheme == "" {
						errs = append(errs, fmt.Errorf("%s.url: invalid URL", p))
					}
				}
			}
		}
	}
	if _, ok := card["capabilities"].(map[string]any); !ok {
		errs = append(errs, fmt.Errorf("capabilities: required object"))
	}
	for _, key := range []string{"defaultInputModes", "defaultOutputModes", "skills"} {
		if value, ok := card[key].([]any); !ok {
			errs = append(errs, fmt.Errorf("%s: required array", key))
		} else if key != "skills" && len(value) == 0 {
			errs = append(errs, fmt.Errorf("%s: must not be empty", key))
		}
	}
	if skills, ok := card["skills"].([]any); ok {
		for i, raw := range skills {
			skill, ok := raw.(map[string]any)
			if !ok {
				errs = append(errs, fmt.Errorf("skills[%d]: must be an object", i))
				continue
			}
			for _, key := range []string{"id", "name", "description"} {
				if value, ok := skill[key].(string); !ok || value == "" {
					errs = append(errs, fmt.Errorf("skills[%d].%s: required", i, key))
				}
			}
			if _, ok := skill["tags"].([]any); !ok {
				errs = append(errs, fmt.Errorf("skills[%d].tags: required array", i))
			}
		}
	}
	return errors.Join(errs...)
}

type Binding struct {
	Module, Repository, Ref string
	Agent                   manifest.Agent
	ModuleDescription       string
}

func Export(base map[string]any, binding Binding) (map[string]any, error) {
	var card map[string]any
	if base != nil {
		raw, _ := json.Marshal(base)
		_ = json.Unmarshal(raw, &card)
	} else {
		var interfaces []any
		for _, contact := range binding.Agent.Contacts {
			if contact.Kind == "a2a" && contact.URL != "" {
				interfaces = append(interfaces, map[string]any{"url": contact.URL, "protocolBinding": "JSONRPC", "protocolVersion": "1.0"})
			}
		}
		if len(interfaces) == 0 {
			return nil, fmt.Errorf("agent %s has neither a card nor an a2a contact", binding.Agent.Name)
		}
		description := binding.Agent.Description
		if description == "" {
			description = binding.ModuleDescription
		}
		if description == "" {
			description = binding.Agent.Name + " agent"
		}
		card = map[string]any{"name": binding.Agent.Name, "description": description, "version": "1.0.0", "supportedInterfaces": interfaces, "capabilities": map[string]any{}, "defaultInputModes": []any{"text/plain"}, "defaultOutputModes": []any{"text/plain"}, "skills": []any{}}
	}
	normalizeLegacy(card)
	if interfaces, ok := card["supportedInterfaces"].([]any); ok {
		for _, raw := range interfaces {
			if item, ok := raw.(map[string]any); ok {
				item["protocolVersion"] = "1.0"
			}
		}
	}
	capabilities, ok := card["capabilities"].(map[string]any)
	if !ok {
		capabilities = map[string]any{}
		card["capabilities"] = capabilities
	}
	var extensions []any
	if current, ok := capabilities["extensions"].([]any); ok {
		for _, raw := range current {
			extension, ok := raw.(map[string]any)
			if !ok || extension["uri"] != ExtensionURI {
				extensions = append(extensions, raw)
			}
		}
	}
	scope := binding.Agent.Scope
	if len(scope) == 0 {
		scope = []string{"**"}
	}
	params := map[string]any{"module": binding.Module, "repository": binding.Repository, "role": binding.Agent.Role, "scope": scope}
	if binding.Ref != "" {
		params["ref"] = binding.Ref
	}
	extensions = append(extensions, map[string]any{"uri": ExtensionURI, "required": false, "params": params})
	capabilities["extensions"] = extensions
	if err := Validate(card); err != nil {
		return nil, err
	}
	return card, nil
}

func Marshal(card map[string]any) ([]byte, error) {
	b, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

type PathReader func(path string) ([]byte, error)

func Snapshot(m *manifest.Manifest, dir string, readPath PathReader) (map[string]string, []error) {
	hashes := map[string]string{}
	var warnings []error
	for _, agent := range m.Agents {
		if agent.Card == "" {
			continue
		}
		var raw []byte
		var err error
		if strings.HasPrefix(agent.Card, "http://") || strings.HasPrefix(agent.Card, "https://") {
			_, raw, err = Read(agent.Card, "")
		} else if readPath != nil {
			raw, err = readPath(agent.Card)
			if err == nil {
				_, err = Parse(raw)
			}
		} else {
			err = fmt.Errorf("relative card %s cannot be read", agent.Card)
		}
		if err != nil {
			warnings = append(warnings, fmt.Errorf("%s: %w", agent.Name, err))
			continue
		}
		card, parseErr := Parse(raw)
		if parseErr != nil {
			warnings = append(warnings, fmt.Errorf("%s: %w", agent.Name, parseErr))
			continue
		}
		if name, _ := card["name"].(string); name != agent.Name {
			warnings = append(warnings, fmt.Errorf("%s: card name is %q", agent.Name, name))
			continue
		}
		if err = os.MkdirAll(dir, 0o755); err != nil {
			warnings = append(warnings, err)
			continue
		}
		if err = os.WriteFile(filepath.Join(dir, FileName(agent.Name)), raw, 0o644); err != nil {
			warnings = append(warnings, err)
			continue
		}
		sum := sha256.Sum256(raw)
		hashes[agent.Name] = "sha256:" + hex.EncodeToString(sum[:])
	}
	return hashes, warnings
}

var unsafeFile = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func FileName(name string) string {
	safe := unsafeFile.ReplaceAllString(name, "_")
	safe = strings.Trim(safe, ".")
	if safe == "" {
		safe = "agent"
	}
	return safe + ".json"
}
