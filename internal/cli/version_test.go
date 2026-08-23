package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVersionDoesNotCheckNetworkWithoutFlag(t *testing.T) {
	oldVersion, oldChannel, oldAPI := Version, Channel, releaseAPI
	defer func() { Version, Channel, releaseAPI = oldVersion, oldChannel, oldAPI }()
	Version = "1.0.0"
	Channel = "go"
	releaseAPI = "http://127.0.0.1:1"
	var out, errOut bytes.Buffer
	code := New(&out, &errOut).Run([]string{"version"})
	if code != 0 || !strings.Contains(out.String(), "channel=go") {
		t.Fatalf("exit %d out=%q err=%q", code, out.String(), errOut.String())
	}
}

func TestVersionCheckReportsChannelUpdateCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.3"}`))
	}))
	defer server.Close()
	oldVersion, oldChannel, oldAPI := Version, Channel, releaseAPI
	defer func() { Version, Channel, releaseAPI = oldVersion, oldChannel, oldAPI }()
	Version, Channel, releaseAPI = "1.0.0", "npm", server.URL
	var out, errOut bytes.Buffer
	code := New(&out, &errOut).Run([]string{"version", "--check"})
	if code != 1 || !strings.Contains(out.String(), "npm install -g git-a2a@latest") {
		t.Fatalf("exit %d out=%q err=%q", code, out.String(), errOut.String())
	}
}

func TestVersionCheckDoesNotTreatPrereleaseAsLatest(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	oldVersion, oldAPI := Version, releaseAPI
	defer func() { Version, releaseAPI = oldVersion, oldAPI }()
	Version, releaseAPI = "1.0.0", server.URL
	var out, errOut bytes.Buffer
	code := New(&out, &errOut).Run([]string{"version", "--check"})
	if code != 0 || !strings.Contains(errOut.String(), "prereleases are not treated as latest") {
		t.Fatalf("exit %d out=%q err=%q", code, out.String(), errOut.String())
	}
}

func TestVersionCheckNeverOffersStableDowngrade(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.1.0"}`))
	}))
	defer server.Close()
	oldVersion, oldChannel, oldAPI := Version, Channel, releaseAPI
	defer func() { Version, Channel, releaseAPI = oldVersion, oldChannel, oldAPI }()
	Version, Channel, releaseAPI = "1.2.0", "binary", server.URL
	var out, errOut bytes.Buffer
	code := New(&out, &errOut).Run([]string{"version", "--check"})
	if code != 0 || !strings.Contains(errOut.String(), "latest stable: 1.1.0") {
		t.Fatalf("exit %d out=%q err=%q", code, out.String(), errOut.String())
	}
	if strings.Contains(out.String(), "update:") || strings.Contains(errOut.String(), "available") {
		t.Fatalf("version check offered a downgrade: out=%q err=%q", out.String(), errOut.String())
	}
}

func TestCompareCoreVersionsUsesNumericSemVerOrder(t *testing.T) {
	if comparison, ok := compareCoreVersions("1.10.0", "1.9.9"); !ok || comparison <= 0 {
		t.Fatalf("comparison=%d comparable=%t", comparison, ok)
	}
	if _, ok := compareCoreVersions("1.2.0-rc.1", "1.1.0"); ok {
		t.Fatal("prerelease text unexpectedly parsed as a canonical core version")
	}
}

func TestUpgradeArchiveChecksumAndExtraction(t *testing.T) {
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	binary := []byte("new binary")
	if err := tw.WriteHeader(&tar.Header{Name: "git-a2a", Mode: 0o755, Size: int64(len(binary))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	name := "git-a2a_1.2.3_linux_amd64.tar.gz"
	sum := sha256.Sum256(archive.Bytes())
	checksums := []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")
	if err := verifyChecksum(name, archive.Bytes(), checksums); err != nil {
		t.Fatal(err)
	}
	got, err := extractBinary(name, archive.Bytes())
	if err != nil || !bytes.Equal(got, binary) {
		t.Fatalf("binary=%q err=%v", got, err)
	}
	if err := verifyChecksum(name, []byte("corrupt"), checksums); err == nil {
		t.Fatal("corrupt archive passed checksum verification")
	}
}
func TestUpgradeRefusesManagedChannel(t *testing.T) {
	old := Channel
	defer func() { Channel = old }()
	Channel = "brew"
	var out, errOut bytes.Buffer
	code := New(&out, &errOut).Run([]string{"upgrade"})
	if code != 1 || !strings.Contains(errOut.String(), "brew upgrade git-a2a") {
		t.Fatalf("exit %d err=%q", code, errOut.String())
	}
}

func TestUpgradeBackupPathIsAdjacentToExecutable(t *testing.T) {
	if got, want := upgradeBackupPath(`C:\tools\git-a2a.exe`), `C:\tools\git-a2a.exe.old`; got != want {
		t.Fatalf("upgradeBackupPath() = %q, want %q", got, want)
	}
}

func TestGlobalTimeoutOption(t *testing.T) {
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	if app.Timeout != 120*time.Second {
		t.Fatalf("default timeout = %s", app.Timeout)
	}
	if code := app.Run([]string{"version", "--timeout", "250ms"}); code != 0 || app.Timeout != 250*time.Millisecond {
		t.Fatalf("exit=%d timeout=%s err=%s", code, app.Timeout, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"--timeout=never", "version"}); code != 2 || !strings.Contains(errOut.String(), "invalid --timeout") {
		t.Fatalf("invalid timeout exit=%d err=%q", code, errOut.String())
	}
}
