package fetch

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/neprel/git-a2a/internal/gitx"
)

func TestPublicHostFetchStrategies(t *testing.T) {
	if os.Getenv("GITA2A_NET") != "1" {
		t.Skip("set GITA2A_NET=1 to exercise public git hosts")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fetcher := Fetcher{Runner: gitx.ExecRunner{Timeout: 2 * time.Minute}}

	tests := []struct {
		name, url, ref, file, method string
	}{
		{"github rejects upload-archive and falls back to sparse", envOr("GITA2A_NET_GITHUB_URL", "https://github.com/a2aproject/A2A.git"), "main", "specification/a2a.proto", "sparse"},
		{"gitlab accepts upload-archive", envOr("GITA2A_NET_ARCHIVE_URL", "git@gitlab.com:gitlab-org/cli.git"), "main", "README.md", "archive"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := fetcher.FetchFile(ctx, test.url, test.ref, test.file, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if result.Method != test.method || len(result.Manifest) == 0 {
				t.Fatalf("method=%q bytes=%d, want method=%q", result.Method, len(result.Manifest), test.method)
			}
		})
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
