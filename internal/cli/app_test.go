package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpExposesExactlyFiveDomainCommands(t *testing.T) {
	var out, err bytes.Buffer
	a := New(&out, &err)
	if code := a.Run([]string{"--help"}); code != 0 {
		t.Fatal(code)
	}
	text := out.String()
	for _, name := range []string{"init", "add", "pull", "remove", "list"} {
		if !strings.Contains(text, name) {
			t.Errorf("missing %s", name)
		}
	}
	for _, old := range []string{"help", "update", "wire", "who", "contact", "card", "catalog", "trust", "setup", "mcp", "upgrade", "doctor", "validate", "status"} {
		if strings.Contains(text, "  "+old) {
			t.Errorf("legacy command exposed: %s", old)
		}
	}
}
func TestLegacyCommandsAreUnknown(t *testing.T) {
	for _, name := range []string{"help", "update", "install", "restore", "show", "validate", "status", "who", "contact", "card", "catalog", "trust", "setup", "mcp", "upgrade", "wire", "fetch", "sync", "doctor", "pin", "unpin"} {
		var out, err bytes.Buffer
		a := New(&out, &err)
		if code := a.Run([]string{name}); code != 2 || !strings.Contains(err.String(), "unknown command") {
			t.Errorf("%s: code=%d err=%q", name, code, err.String())
		}
	}
}
func TestInitCreatesIncompleteSchema2(t *testing.T) {
	root := t.TempDir()
	var out, err bytes.Buffer
	a := New(&out, &err)
	a.Root = root
	if code := a.Run([]string{"init", "--id", "consumer"}); code != 0 {
		t.Fatalf("code=%d err=%s", code, err.String())
	}
	if !strings.Contains(err.String(), "agent.card") {
		t.Fatalf("missing publication warning: %s", err.String())
	}
}
