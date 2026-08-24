package a2a

import (
	"fmt"
	"net/url"
	"strings"
)

// CheckOrigins binds card discovery and interface URLs to the module repository
// or to explicit owner-declared origins.
func CheckOrigins(raw []byte, cardURL, repository, canonicalGit string, declared []string) error {
	card, err := decodeCard(raw)
	if err != nil {
		return err
	}
	if extensionRepository(card) != "" && sameGitIdentity(extensionRepository(card), canonicalGit) {
		return nil
	}
	locations := []string{cardURL}
	if interfaces, ok := card["supportedInterfaces"].([]any); ok {
		for _, item := range interfaces {
			if binding, ok := item.(map[string]any); ok {
				if location, _ := binding["url"].(string); location != "" {
					locations = append(locations, location)
				}
			}
		}
	}
	for _, location := range locations {
		if location == "" || !strings.Contains(location, "://") {
			continue
		}
		if len(declared) > 0 {
			if !originIn(location, declared) {
				return fmt.Errorf("trust: origin mismatch: %s is not in trust.origins", location)
			}
			continue
		}
		if !sameRepositoryHost(location, repository) {
			return fmt.Errorf("trust: origin mismatch: %s does not match module.repository", location)
		}
	}
	return nil
}

func originIn(location string, declared []string) bool {
	parsed, err := url.Parse(location)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	origin := strings.ToLower(parsed.Scheme + "://" + parsed.Host)
	for _, allowed := range declared {
		if origin == strings.ToLower(strings.TrimSuffix(allowed, "/")) {
			return true
		}
	}
	return false
}

func sameRepositoryHost(location, repository string) bool {
	parsed, err := url.Parse(location)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), repositoryHost(repository))
}

func repositoryHost(repository string) string {
	if parsed, err := url.Parse(repository); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	if at := strings.LastIndex(repository, "@"); at >= 0 {
		repository = repository[at+1:]
	}
	if colon := strings.Index(repository, ":"); colon >= 0 {
		return strings.Trim(repository[:colon], "[]")
	}
	return ""
}

func extensionRepository(card map[string]any) string {
	capabilities, _ := card["capabilities"].(map[string]any)
	extensions, _ := capabilities["extensions"].([]any)
	for _, item := range extensions {
		extension, _ := item.(map[string]any)
		if extension["uri"] != "https://git-a2a.com/ext/module/v1" {
			continue
		}
		params, _ := extension["params"].(map[string]any)
		repository, _ := params["repository"].(string)
		return repository
	}
	return ""
}

func sameGitIdentity(left, right string) bool {
	normalize := func(value string) string {
		value = strings.TrimSpace(strings.TrimPrefix(value, "git+"))
		if strings.HasPrefix(value, "git@") {
			value = strings.TrimPrefix(value, "git@")
			value = strings.Replace(value, ":", "/", 1)
		} else if marker := strings.Index(value, "://"); marker >= 0 {
			value = strings.TrimPrefix(value[marker+3:], "git@")
		}
		value = strings.TrimSuffix(strings.TrimSuffix(value, "/"), ".git")
		return strings.ToLower(value)
	}
	return left != "" && right != "" && normalize(left) == normalize(right)
}
