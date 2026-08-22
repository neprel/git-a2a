package catalog

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/neprel/git-a2a/internal/a2a"
	"github.com/neprel/git-a2a/internal/manifest"
)

const (
	SpecVersion      = "1.0"
	A2AAgentCardType = "application/a2a-agent-card+json"
)

type Catalog struct {
	SpecVersion string  `json:"specVersion"`
	Host        Host    `json:"host"`
	Entries     []Entry `json:"entries"`
}

type Host struct {
	DisplayName string `json:"displayName"`
	Identifier  string `json:"identifier,omitempty"`
}

type Entry struct {
	Identifier  string         `json:"identifier"`
	DisplayName string         `json:"displayName"`
	Type        string         `json:"type"`
	URL         string         `json:"url,omitempty"`
	Data        map[string]any `json:"data,omitempty"`
}

func Build(module *manifest.Manifest, root, repository, ref string) (*Catalog, error) {
	publisher, namespace, err := repositoryIdentity(repository)
	if err != nil {
		return nil, err
	}
	displayName := module.Module.Name
	if displayName == "" {
		displayName = module.Module.ID
	}
	result := &Catalog{SpecVersion: SpecVersion, Host: Host{DisplayName: displayName, Identifier: publisher}}
	for _, agent := range module.Agents {
		entry := Entry{
			Identifier:  "urn:air:" + publisher + ":" + namespace + ":" + segment(agent.Name),
			DisplayName: agent.Name,
			Type:        A2AAgentCardType,
		}
		switch {
		case strings.HasPrefix(agent.Card, "http://") || strings.HasPrefix(agent.Card, "https://"):
			parsed, parseErr := url.Parse(agent.Card)
			if parseErr != nil || parsed.Host == "" || parsed.User != nil {
				return nil, fmt.Errorf("agent %s card is not a public HTTP(S) URL", agent.Name)
			}
			entry.URL = agent.Card
		case agent.Card != "":
			raw, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(agent.Card)))
			if readErr != nil {
				return nil, fmt.Errorf("agent %s card: %w", agent.Name, readErr)
			}
			entry.Data, readErr = a2a.Parse(raw)
			if readErr != nil {
				return nil, fmt.Errorf("agent %s card: %w", agent.Name, readErr)
			}
		default:
			entry.Data, err = a2a.Export(nil, a2a.Binding{
				Module: module.Module.ID, Repository: repository, Ref: ref,
				Agent: agent, ModuleDescription: module.Module.Description,
			})
			if err != nil {
				return nil, fmt.Errorf("agent %s has no exportable A2A card: %w", agent.Name, err)
			}
		}
		result.Entries = append(result.Entries, entry)
	}
	if len(result.Entries) == 0 {
		return nil, fmt.Errorf("module has no agents")
	}
	if err = Validate(result); err != nil {
		return nil, err
	}
	return result, nil
}

func Marshal(value *Catalog) ([]byte, error) {
	if err := Validate(value); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

var identifierPattern = regexp.MustCompile(`^urn:air:[A-Za-z0-9.-]+(:[A-Za-z0-9._-]+)+$`)

func Validate(value *Catalog) error {
	if value == nil || value.SpecVersion != SpecVersion {
		return fmt.Errorf("catalog specVersion must be %s", SpecVersion)
	}
	if value.Host.DisplayName == "" {
		return fmt.Errorf("catalog host displayName is required")
	}
	for index, entry := range value.Entries {
		prefix := fmt.Sprintf("entries[%d]", index)
		if !identifierPattern.MatchString(entry.Identifier) {
			return fmt.Errorf("%s identifier is not an ARD urn:air identifier", prefix)
		}
		if entry.DisplayName == "" || entry.Type == "" {
			return fmt.Errorf("%s displayName and type are required", prefix)
		}
		if (entry.URL == "") == (entry.Data == nil) {
			return fmt.Errorf("%s must contain exactly one of url or data", prefix)
		}
		if entry.URL != "" {
			parsed, err := url.Parse(entry.URL)
			if err != nil || parsed.Scheme == "" || parsed.Host == "" {
				return fmt.Errorf("%s url must be an absolute URI", prefix)
			}
		}
	}
	return nil
}

func repositoryIdentity(repository string) (string, string, error) {
	raw := strings.TrimSpace(repository)
	host, path := "", ""
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err == nil {
			host = parsed.Hostname()
			path = parsed.Path
		}
	} else if at := strings.LastIndex(raw, "@"); at >= 0 {
		parts := strings.SplitN(raw[at+1:], ":", 2)
		if len(parts) == 2 {
			host, path = parts[0], parts[1]
		}
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || !strings.Contains(host, ".") || segment(host) != host {
		return "", "", fmt.Errorf("module repository must have a verifiable DNS host for ARD identifiers")
	}
	path = strings.Trim(strings.TrimSuffix(path, ".git"), "/")
	namespace := segment(strings.ReplaceAll(path, "/", "-"))
	if namespace == "item" {
		return "", "", fmt.Errorf("module repository path is required for ARD identifiers")
	}
	return host, namespace, nil
}

var invalidSegment = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func segment(value string) string {
	value = strings.ToLower(invalidSegment.ReplaceAllString(value, "-"))
	value = strings.Trim(value, ".-_")
	if value == "" {
		return "item"
	}
	return value
}
