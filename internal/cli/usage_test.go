package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestUsageIsDeterministicCompactAndComplete(t *testing.T) {
	var first, second, errOut bytes.Buffer
	if code := New(&first, &errOut).Run([]string{"usage"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if code := New(&second, &errOut).Run([]string{"usage"}); code != 0 || first.String() != second.String() {
		t.Fatal("usage output is not deterministic")
	}
	lines := strings.Split(strings.TrimSuffix(first.String(), "\n"), "\n")
	if len(lines) > 60 {
		t.Fatalf("compact usage has %d lines", len(lines))
	}
	for _, command := range []string{"init", "add", "fetch", "sync", "status", "update", "who", "contact"} {
		if !strings.Contains(first.String(), "git-a2a "+command) {
			t.Errorf("usage omits %s example", command)
		}
	}
	for _, required := range []string{"Exit 0:", "Exit 1:", "Exit 2:", "--json", "--vendor submodule|copy", "manifest-reference.md"} {
		if !strings.Contains(first.String(), required) {
			t.Errorf("usage omits %q", required)
		}
	}
}

func TestUsagePromptAndJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := New(&out, &errOut).Run([]string{"usage", "--prompt", "--json"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var value usageOutput
	if err := json.Unmarshal(out.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if !value.Prompt || value.LineCount <= strings.Count(compactBriefing, "\n") || !strings.Contains(strings.Join(value.Lines, "\n"), "Fresh-agent workflow") {
		t.Fatalf("prompt JSON = %#v", value)
	}
}
