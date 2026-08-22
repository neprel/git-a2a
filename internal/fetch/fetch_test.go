package fetch

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if len(r.calls) != 2 {
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
