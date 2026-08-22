package routing

import (
	"github.com/neprel/git-a2a/internal/manifest"
	"testing"
)

func TestResolveMatrix(t *testing.T) {
	m := &manifest.Manifest{Agents: []manifest.Agent{{Name: "owner", Role: "owner", Scope: []string{"**"}, Contacts: []manifest.Contact{{Intents: []string{"*"}, Kind: "email", Address: "owner@example.com"}}}, {Name: "specific", Role: "owner", Scope: []string{"src/**"}, Contacts: []manifest.Contact{{Intents: []string{"bug"}, Kind: "url", URL: "https://example.com/bugs"}}}, {Name: "spec", Role: "spec", Contacts: []manifest.Contact{{Intents: []string{"change"}, Kind: "github-issue", Repo: "acme/lib"}}}}, Policy: &manifest.Policy{Intents: map[string]string{"change": "spec"}}}
	got, role := Resolve(m, "change", "")
	if role != "spec" || len(got) != 1 || got[0].Agent.Name != "spec" {
		t.Fatalf("change: %s %#v", role, got)
	}
	got, _ = Resolve(m, "bug", "src/x.go")
	if len(got) != 2 || got[0].Agent.Name != "specific" {
		t.Fatalf("scope ordering: %#v", got)
	}
	got, _ = Resolve(m, "incident", "README.md")
	if len(got) != 1 || got[0].Contacts[0].Kind != "email" {
		t.Fatalf("fallback: %#v", got)
	}
}

func TestContactTextRendersUnknownFieldsDeterministically(t *testing.T) {
	contact := manifest.Contact{Kind: "pager-duty", Extensions: map[string]any{"service": "checkout", "priority": 2}}
	if got, want := ContactText(contact), `kind=pager-duty priority=2 service="checkout"`; got != want {
		t.Fatalf("ContactText() = %q, want %q", got, want)
	}
}
