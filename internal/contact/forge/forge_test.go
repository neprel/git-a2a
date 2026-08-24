package forge

import (
	"net/url"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/manifest"
)

func TestDeepLinkEscapesUntrustedTitleAndBody(t *testing.T) {
	message := "# break & title\nbody ?&=<script>"
	tests := []struct {
		name, kind, path, titleKey, bodyKey string
		contact                             manifest.Contact
	}{
		{"GitHub", "github-issue", "/acme/lib/issues/new", "title", "body", manifest.Contact{Repo: "acme/lib"}},
		{"GitLab", "gitlab-issue", "/group/sub/acme-lib/-/issues/new", "issue[title]", "issue[description]", manifest.Contact{Repo: "group/sub/acme-lib", Server: "gitlab.example.test"}},
		{"Gitea", "gitea-issue", "/acme/lib/issues/new", "title", "body", manifest.Contact{Repo: "acme/lib", Server: "codeberg.org"}},
		{"Bitbucket", "bitbucket-issue", "/acme/lib/issues/new", "title", "body", manifest.Contact{Repo: "acme/lib"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			link := DeepLink(test.kind, test.contact, message)
			parsed, err := url.Parse(link)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Path != test.path {
				t.Fatalf("path = %q", parsed.Path)
			}
			if got := parsed.Query().Get(test.titleKey); got != "break & title" {
				t.Fatalf("title = %q", got)
			}
			if got := parsed.Query().Get(test.bodyKey); got != message {
				t.Fatalf("body = %q", got)
			}
			if strings.Contains(parsed.RawQuery, "<script>") {
				t.Fatalf("query is not context encoded: %s", parsed.RawQuery)
			}
			if test.kind == "gitlab-issue" {
				t.Logf("GitLab deep link: %s", link)
			}
		})
	}

	link := DeepLink("gitlab-issue", tests[1].contact, message+strings.Repeat("x", 3000))
	if len(link) > maxDeepLinkBytes {
		t.Fatalf("deep link length = %d", len(link))
	}
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/group/sub/acme-lib/-/issues/new" {
		t.Fatalf("path = %q", parsed.Path)
	}
	if got := parsed.Query().Get("issue[title]"); got != "break & title" {
		t.Fatalf("title = %q", got)
	}
	if strings.Contains(parsed.RawQuery, "<script>") || !strings.Contains(parsed.Query().Get("issue[description]"), "<script>") {
		t.Fatalf("query is not context encoded: %s", parsed.RawQuery)
	}
}
