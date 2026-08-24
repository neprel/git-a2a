package forge

import (
	"net/url"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/manifest"
)

func TestDeepLinkEscapesUntrustedTitleAndBody(t *testing.T) {
	message := "# break & title\nbody ?&=<script>" + strings.Repeat("x", 3000)
	link := DeepLink("gitlab-issue", manifest.Contact{Repo: "group/sub/acme-lib", Server: "gitlab.example.test"}, message)
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
	if got := parsed.Query().Get("title"); got != "break & title" {
		t.Fatalf("title = %q", got)
	}
	if strings.Contains(parsed.RawQuery, "<script>") || !strings.Contains(parsed.Query().Get("body"), "<script>") {
		t.Fatalf("query is not context encoded: %s", parsed.RawQuery)
	}
}
