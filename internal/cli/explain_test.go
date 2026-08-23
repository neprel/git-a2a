package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestExplainResolvesConvenientArrayPathAndJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	if code := app.Run([]string{"explain", "agents.contacts.kind"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.HasPrefix(out.String(), "## `agents[].contacts[].kind`\n") || !strings.Contains(out.String(), "Known values:") {
		t.Fatalf("output:\n%s", out.String())
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"explain", "module.id", "--json"}); code != 0 {
		t.Fatalf("json exit %d: %s", code, errOut.String())
	}
	var value explainOutput
	if err := json.Unmarshal(out.Bytes(), &value); err != nil || value.Path != "module.id" || !strings.Contains(value.Markdown, "breaking change") {
		t.Fatalf("json = %s, %v", out.String(), err)
	}
}

func TestExplainRejectsUnknownPath(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := New(&out, &errOut).Run([]string{"explain", "module.unknown"}); code != 2 {
		t.Fatalf("exit %d", code)
	}
}
