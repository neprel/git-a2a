package plugin

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/neprel/git-a2a/internal/contact"
	"github.com/neprel/git-a2a/internal/manifest"
)

func TestPluginProtocolRoundTripAndStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX protocol fixture")
	}
	directory := t.TempDir()
	executable := filepath.Join(directory, "git-a2a-contact-acme-tracker")
	script := `#!/bin/sh
payload=$(cat)
case "$payload" in
  *'"protocol":1'*'"kind":"acme-tracker"'*'"module":"acme-lib"'*'"message":"hello"'*) ;;
  *) echo "bad payload: $payload" >&2; exit 1 ;;
esac
echo progress >&2
printf '%s\n' '{"id":"ACME-42","state":"created","url":"https://tracker.example/42","note":"queued"}'
`
	if err := os.WriteFile(executable, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var diagnostics bytes.Buffer
	driver := Driver{ContactKind: "acme-tracker", Executable: executable, Stderr: &diagnostics}
	record, err := driver.Deliver(context.Background(), contact.Request{Intent: "change", Module: "acme-lib", Origin: "https://git.example/acme/lib", Message: "hello", Contact: manifest.Contact{Kind: "acme-tracker", Extensions: map[string]any{"queue": "platform"}}})
	if err != nil {
		t.Fatal(err)
	}
	if record.Driver != "plugin:git-a2a-contact-acme-tracker" || record.ID != "ACME-42" || record.State != "created" || record.URL == "" {
		t.Fatalf("record=%#v", record)
	}
	if got := diagnostics.String(); got != "plugin git-a2a-contact-acme-tracker: progress\n" {
		t.Fatalf("stderr=%q", got)
	}
}

func TestPluginTimeoutAndRefusal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX timing fixture")
	}
	directory := t.TempDir()
	timeoutPlugin := filepath.Join(directory, "timeout")
	if err := os.WriteFile(timeoutPlugin, []byte("#!/bin/sh\nsleep 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := (Driver{ContactKind: "slow", Executable: timeoutPlugin, Timeout: 20 * time.Millisecond}).Deliver(context.Background(), contact.Request{})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error=%v", err)
	}

	refusingPlugin := filepath.Join(directory, "refuse")
	if err := os.WriteFile(refusingPlugin, []byte("#!/bin/sh\ncat >/dev/null\nexit 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = (Driver{ContactKind: "refusing", Executable: refusingPlugin}).Deliver(context.Background(), contact.Request{})
	if err == nil || !strings.Contains(err.Error(), "refused request") {
		t.Fatalf("error=%v", err)
	}
}

func TestPluginWindowsCMDStub(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows .cmd contract")
	}
	directory := t.TempDir()
	executable := filepath.Join(directory, "git-a2a-contact-acme-tracker.cmd")
	script := "@echo off\r\nset /p payload=\r\necho {\"id\":\"WIN-1\",\"state\":\"sent\"}\r\n"
	if err := os.WriteFile(executable, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	found, err := Find("acme-tracker")
	if err != nil {
		t.Fatal(err)
	}
	record, err := (Driver{ContactKind: "acme-tracker", Executable: found}).Deliver(context.Background(), contact.Request{})
	if err != nil || record.ID != "WIN-1" {
		t.Fatalf("record=%#v err=%v", record, err)
	}
}
