package fetch

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/gitx"
)

type fakeRunner struct {
	tar   []byte
	calls []string
}

type fileFallbackRunner struct{ body []byte }

func (r fileFallbackRunner) Run(_ context.Context, dir string, _ []byte, args ...string) ([]byte, error) {
	switch args[0] {
	case "archive":
		return nil, fmt.Errorf("archive unsupported")
	case "clone":
		return nil, os.MkdirAll(filepath.Join(args[len(args)-1], ".git"), 0o755)
	case "sparse-checkout", "fetch":
		return nil, nil
	case "checkout":
		p := filepath.Join(dir, "cards", "agent.json")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(p, r.body, 0o644)
	default:
		return nil, fmt.Errorf("unexpected command %s", args[0])
	}
}

func (f *fakeRunner) Run(_ context.Context, _ string, _ []byte, args ...string) ([]byte, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	switch args[0] {
	case "ls-remote":
		return []byte(strings.Repeat("a", 40) + "\trefs/heads/main\n"), nil
	case "archive":
		return f.tar, nil
	}
	return nil, fmt.Errorf("unexpected")
}

func TestArchiveSelectedFirst(t *testing.T) {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	body := []byte("schema: 1\nmodule: {id: demo}\n")
	_ = tw.WriteHeader(&tar.Header{Name: "a2amodule.yml", Mode: 0o644, Size: int64(len(body))})
	_, _ = tw.Write(body)
	_ = tw.Close()
	r := &fakeRunner{tar: b.Bytes()}
	got, err := (Fetcher{Runner: r}).Fetch(context.Background(), "file:///repo", "main", ".", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != "archive" || !bytes.Equal(got.Manifest, body) {
		t.Fatalf("got %#v", got)
	}
	if len(r.calls) != 3 {
		t.Fatalf("calls: %v", r.calls)
	}
}

func TestFileFallsBackToSparseFetch(t *testing.T) {
	body := []byte(`{"name":"agent"}`)
	got, err := (Fetcher{Runner: fileFallbackRunner{body: body}}).File(context.Background(), "https://example.test/repo.git", strings.Repeat("a", 40), "cards/agent.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("got %q", got)
	}
}

func TestSurfaceFallsBackFromBareArchiveAndReturnsTree(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	bare := filepath.Join(tmp, "library.git")
	if err := os.MkdirAll(filepath.Join(source, "docs", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "init", "-b", "main")
	runGit(t, source, "config", "user.email", "test@example.com")
	runGit(t, source, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(source, "docs", "README.md"), []byte("surface\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "docs", "nested", "api.txt"), []byte("api\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "add", "docs")
	runGit(t, source, "commit", "-m", "surface")
	commit := strings.TrimSpace(runGit(t, source, "rev-parse", "HEAD"))
	wantTree := strings.TrimSpace(runGit(t, source, "rev-parse", "HEAD:docs"))
	runGit(t, tmp, "clone", "--bare", source, bare)

	dest := filepath.Join(tmp, "dest")
	result, err := (Fetcher{Runner: gitx.ExecRunner{}}).Surface(context.Background(), "file://"+bare, commit, ".", "docs", dest, filepath.Join(tmp, "work"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Method == "archive" {
		t.Fatal("bare upload-archive unexpectedly accepted an object id; fallback was not exercised")
	}
	if result.Tree != "tree:"+wantTree {
		t.Fatalf("tree = %q, want tree:%s", result.Tree, wantTree)
	}
	if got, want := strings.Join(result.Files, ","), "README.md,nested/api.txt"; got != want {
		t.Fatalf("files = %q, want %q", got, want)
	}
	if body, readErr := os.ReadFile(filepath.Join(dest, "nested", "api.txt")); readErr != nil || strings.ReplaceAll(string(body), "\r\n", "\n") != "api\n" {
		t.Fatalf("copied surface = %q, err=%v", body, readErr)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}
