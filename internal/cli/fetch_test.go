package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	lockfile "github.com/neprel/git-a2a/internal/lock"
	"github.com/neprel/git-a2a/internal/manifest"
)

func TestFetchRestoresLockedCacheAndSurfaceWithoutChangingDurableState(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.autocrlf")
	t.Setenv("GIT_CONFIG_VALUE_0", "true")
	root, remote, commit, manifestRaw, surfaceTree := fetchFixture(t)
	writeFetchConsumer(t, root, remote, commit, manifestRaw, surfaceTree)
	manifestBefore := mustRead(t, filepath.Join(root, "a2amodule.yml"))
	lockBefore := mustRead(t, filepath.Join(root, "a2amodule.lock"))
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"fetch", "acme-lib", "--surface", "--json"}); code != 0 {
		t.Fatalf("exit %d\nout=%s\nerr=%s", code, out.String(), errOut.String())
	}
	var records []fetchResult
	if err := json.Unmarshal(out.Bytes(), &records); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out.String())
	}
	if len(records) != 1 || records[0].Commit != commit || records[0].Surface != surfaceTree {
		t.Fatalf("records = %#v", records)
	}
	if got := mustRead(t, filepath.Join(root, ".git-a2a", "cache", "acme-lib", "a2amodule.yml")); !bytes.Equal(got, manifestRaw) {
		t.Fatalf("cached manifest differs:\n%s", got)
	}
	if got := string(mustRead(t, filepath.Join(root, ".git-a2a", "cache", "acme-lib", "surface", "API.md"))); got != "public api\n" {
		t.Fatalf("surface = %q", got)
	}
	if !bytes.Equal(manifestBefore, mustRead(t, filepath.Join(root, "a2amodule.yml"))) || !bytes.Equal(lockBefore, mustRead(t, filepath.Join(root, "a2amodule.lock"))) {
		t.Fatal("fetch changed durable manifest or lock")
	}
}

func TestFetchRejectsMissingLockAndHashMismatchWithoutReplacingCache(t *testing.T) {
	root, remote, commit, manifestRaw, _ := fetchFixture(t)
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"fetch"}); code != 1 || !strings.Contains(errOut.String(), "a2amodule.lock is required") {
		t.Fatalf("missing lock exit=%d err=%q", code, errOut.String())
	}
	writeFetchConsumer(t, root, remote, commit, manifestRaw, "")
	locked, err := lockfile.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	entry := locked.Dependencies["acme-lib"]
	entry.Manifest = "sha256:" + strings.Repeat("0", 64)
	locked.Dependencies["acme-lib"] = entry
	if err = lockfile.Write(root, locked); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(root, ".git-a2a", "cache", "acme-lib", "a2amodule.yml")
	if err = os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(cachePath, []byte("old cache\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"fetch", "acme-lib"}); code != 1 || !strings.Contains(errOut.String(), "locked content mismatch") {
		t.Fatalf("mismatch exit=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	if got := string(mustRead(t, cachePath)); got != "old cache\n" {
		t.Fatalf("failed fetch replaced cache with %q", got)
	}
}

func fetchFixture(t *testing.T) (root, remote, commit string, manifestRaw []byte, surfaceTree string) {
	t.Helper()
	base := t.TempDir()
	source := filepath.Join(base, "source")
	remote = filepath.Join(base, "library.git")
	root = filepath.Join(base, "consumer")
	if err := os.MkdirAll(filepath.Join(source, "surface"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifestRaw = []byte(`schema: 1
module:
  id: acme-lib
  surface: surface
agents:
  - name: acme-lib-owner
    role: owner
    contacts:
      - intents: [question]
        kind: url
        url: https://example.com/acme-lib
policy:
  intents:
    question: owner
`)
	if err := os.WriteFile(filepath.Join(source, "a2amodule.yml"), manifestRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "surface", "API.md"), []byte("public api\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runFetchGit(t, source, "init", "-b", "main")
	runFetchGit(t, source, "config", "user.email", "test@example.com")
	runFetchGit(t, source, "config", "user.name", "Test")
	runFetchGit(t, source, "add", ".")
	runFetchGit(t, source, "commit", "-m", "fixture")
	commit = strings.TrimSpace(runFetchGit(t, source, "rev-parse", "HEAD"))
	surfaceTree = "tree:" + strings.TrimSpace(runFetchGit(t, source, "rev-parse", "HEAD:surface"))
	runFetchGit(t, base, "clone", "--bare", source, remote)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root, "file://" + remote, commit, manifestRaw, surfaceTree
}

func writeFetchConsumer(t *testing.T, root, remote, commit string, manifestRaw []byte, surfaceTree string) {
	t.Helper()
	own := &manifest.Manifest{Schema: 1, Module: manifest.Module{ID: "consumer-app"}, Dependencies: []manifest.Dependency{{ID: "acme-lib", Git: remote, Ref: "main", Path: "."}}}
	ownRaw, err := manifest.Marshal(own)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "a2amodule.yml"), ownRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(manifestRaw)
	if err = lockfile.Write(root, &manifest.Lock{Schema: 1, Dependencies: map[string]manifest.LockedDependency{
		"acme-lib": {Git: remote, Ref: "main", Path: ".", Commit: commit, Manifest: "sha256:" + hex.EncodeToString(sum[:]), Surface: surfaceTree},
	}}); err != nil {
		t.Fatal(err)
	}
}

func runFetchGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1")
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
