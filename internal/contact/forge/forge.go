package forge

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/neprel/git-a2a/internal/contact"
	"github.com/neprel/git-a2a/internal/manifest"
)

const maxDeepLinkBytes = 1800

func Title(message string) string {
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		if line == "" {
			continue
		}
		return truncate(line, 80)
	}
	return "git-a2a contact request"
}

func Body(message string) string { return strings.TrimSpace(message) }

func DeepLink(kind string, declared manifest.Contact, message string) string {
	server := strings.TrimSuffix(strings.TrimSpace(declared.Server), "/")
	var base string
	switch kind {
	case "github-issue":
		if server == "" {
			server = "github.com"
		}
		base = httpsServer(server) + "/" + strings.Trim(declared.Repo, "/") + "/issues/new"
	case "gitlab-issue":
		if server == "" {
			server = "gitlab.com"
		}
		base = httpsServer(server) + "/" + strings.Trim(declared.Repo, "/") + "/-/issues/new"
	case "gitea-issue":
		base = httpsServer(server) + "/" + strings.Trim(declared.Repo, "/") + "/issues/new"
	case "bitbucket-issue":
		base = "https://bitbucket.org/" + strings.Trim(declared.Repo, "/") + "/issues/new"
	default:
		return ""
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" {
		return ""
	}
	query := parsed.Query()
	titleKey, bodyKey := "title", "body"
	if kind == "gitlab-issue" {
		titleKey, bodyKey = "issue[title]", "issue[description]"
	}
	query.Set(titleKey, Title(message))
	query.Set(bodyKey, Body(message))
	parsed.RawQuery = query.Encode()
	result := parsed.String()
	if len(result) <= maxDeepLinkBytes {
		return result
	}
	query.Set(bodyKey, truncateBytes(Body(message), maxDeepLinkBytes-len(base)-len(query.Get(titleKey))-80))
	parsed.RawQuery = query.Encode()
	return truncateBytes(parsed.String(), maxDeepLinkBytes)
}

func Instruction(agent, kind string, declared manifest.Contact, message string) contact.Record {
	link := DeepLink(kind, declared, message)
	text := "create an issue"
	if link != "" {
		text += " using " + link
	} else if declared.Repo != "" {
		text += " in " + declared.Repo
	}
	return contact.Record{Agent: agent, Kind: kind, Driver: "instruction", ID: link, State: "instruction", Instruction: text}
}

func Run(ctx context.Context, executable string, args []string, input string, environment []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdin = strings.NewReader(input)
	if environment != nil {
		cmd.Env = environment
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, Sanitize(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func Sanitize(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 200 {
		runes = runes[:200]
	}
	return string(runes)
}

func httpsServer(server string) string {
	server = strings.TrimSuffix(strings.TrimSpace(server), "/")
	if strings.HasPrefix(server, "https://") {
		return server
	}
	return "https://" + server
}

func truncate(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

func truncateBytes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	for len(value) > limit {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}
