package routing

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/neprel/git-a2a/internal/manifest"
)

type Match struct {
	Agent    manifest.Agent     `json:"agent"`
	Contacts []manifest.Contact `json:"contacts"`
}

func Resolve(m *manifest.Manifest, intent, filePath string) ([]Match, string) {
	role := "owner"
	if m.Policy != nil {
		if declared := m.Policy.Intents[intent]; declared != "" {
			role = declared
		}
	}
	type candidate struct {
		agent manifest.Agent
		score int
		order int
	}
	var candidates []candidate
	for i, a := range m.Agents {
		if a.Role != role {
			continue
		}
		score, ok := scopeScore(a.Scope, filePath)
		if ok {
			candidates = append(candidates, candidate{a, score, i})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].order < candidates[j].order
		}
		return candidates[i].score > candidates[j].score
	})
	var out []Match
	for _, candidate := range candidates {
		var exact, fallback []manifest.Contact
		for _, c := range candidate.agent.Contacts {
			for _, declared := range c.Intents {
				if declared == intent {
					exact = append(exact, c)
					break
				}
				if declared == "*" {
					fallback = append(fallback, c)
					break
				}
			}
		}
		contacts := append(exact, fallback...)
		if len(contacts) > 0 {
			out = append(out, Match{Agent: candidate.agent, Contacts: contacts})
		}
	}
	return out, role
}

func scopeScore(scopes []string, filePath string) (int, bool) {
	if len(scopes) == 0 {
		scopes = []string{"**"}
	}
	if filePath == "" {
		return 0, true
	}
	best := -1
	for _, scope := range scopes {
		if glob(scope, filePath) {
			score := len(strings.NewReplacer("*", "", "?", "").Replace(scope))
			if score > best {
				best = score
			}
		}
	}
	return best, best >= 0
}
func glob(pattern, value string) bool {
	var b strings.Builder
	b.WriteByte('^')
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	b.WriteByte('$')
	ok, _ := regexp.MatchString(b.String(), strings.TrimPrefix(value, "./"))
	return ok
}

func ContactText(c manifest.Contact) string {
	switch c.Kind {
	case "a2a":
		return fmt.Sprintf("A2A %s", c.URL)
	case "email":
		return fmt.Sprintf("email %s", c.Address)
	case "github-issue", "gitlab-issue":
		text := fmt.Sprintf("%s %s", c.Kind, c.Repo)
		if len(c.Labels) > 0 {
			text += " labels=" + strings.Join(c.Labels, ",")
		}
		return text
	case "jira":
		return fmt.Sprintf("Jira %s project %s", c.URL, c.Project)
	case "mattermost", "slack", "discord", "telegram", "teams":
		return fmt.Sprintf("%s channel %s handle %s", c.Kind, c.Channel, c.Handle)
	case "url":
		return c.URL
	default:
		return fmt.Sprintf("kind=%s", c.Kind)
	}
}
