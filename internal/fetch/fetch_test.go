package fetch

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

type fakeRunner struct {
	tar   []byte
	calls []string
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
