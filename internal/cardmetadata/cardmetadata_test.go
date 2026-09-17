package cardmetadata

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/v2/internal/fetch"
	"github.com/neprel/git-a2a/v2/internal/gitx"
	"github.com/neprel/git-a2a/v2/internal/manifest"
)

type recordingFetcher struct {
	body                 []byte
	source, commit, path string
	err                  error
}

func (f *recordingFetcher) File(_ context.Context, source, commit, filePath string) ([]byte, error) {
	f.source, f.commit, f.path = source, commit, filePath
	return append([]byte(nil), f.body...), f.err
}

func TestPrepareRelativeCardUsesManifestRootAndExactCommit(t *testing.T) {
	commit := strings.Repeat("a", 40)
	body := []byte("{\r\n  \"signed\": \"unchanged\"\r\n}\x00")
	fetcher := &recordingFetcher{body: body}
	prepared, err := Prepare(context.Background(), fetcher, "lib", "file:///upstream.git", commit, "modules/lib", manifest.Agent{
		Name: "owner", Card: "cards/agent-card.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fetcher.path != "modules/lib/cards/agent-card.json" || fetcher.commit != commit {
		t.Fatalf("fetch = %s@%s:%s", fetcher.source, fetcher.commit, fetcher.path)
	}
	if prepared.Agent.DeclaredCard != "cards/agent-card.json" || prepared.Agent.Card != ".git-a2a/agents/lib/agent-card.json" || prepared.Agent.Commit != commit {
		t.Fatalf("agent = %+v", prepared.Agent)
	}
	root := t.TempDir()
	restore, finalize, err := Apply(root, "lib", prepared)
	if err != nil {
		t.Fatal(err)
	}
	defer finalize()
	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(prepared.Agent.Card)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("materialized bytes changed: %q", got)
	}
	if err := restore(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(prepared.Agent.Card))); !os.IsNotExist(err) {
		t.Fatalf("rollback left card: %v", err)
	}
}

func TestRelativeTransitionsAndOfflineRecovery(t *testing.T) {
	root := t.TempDir()
	commitA := strings.Repeat("a", 40)
	commitB := strings.Repeat("b", 40)
	fetcher := &recordingFetcher{body: []byte("first")}
	first, err := Prepare(context.Background(), fetcher, "lib", "g", commitA, ".", manifest.Agent{Card: "one.json"})
	if err != nil {
		t.Fatal(err)
	}
	_, finalize, err := Apply(root, "lib", first)
	if err != nil {
		t.Fatal(err)
	}
	finalize()

	fetcher.body = []byte("second")
	second, err := Prepare(context.Background(), fetcher, "lib", "g", commitB, ".", manifest.Agent{Card: "two.json"})
	if err != nil {
		t.Fatal(err)
	}
	_, finalize, err = Apply(root, "lib", second)
	if err != nil {
		t.Fatal(err)
	}
	finalize()
	listed, problems := ForInspection(root, "lib", second.Agent)
	if len(problems) != 0 || listed.Card == "" || listed.DeclaredCard != "two.json" || listed.Commit != commitB {
		t.Fatalf("listed=%+v problems=%v", listed, problems)
	}
	got, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(listed.Card)))
	if string(got) != "second" {
		t.Fatalf("card = %q", got)
	}

	if err := os.Remove(filepath.Join(root, filepath.FromSlash(listed.Card))); err != nil {
		t.Fatal(err)
	}
	listed, problems = ForInspection(root, "lib", second.Agent)
	if listed.Card != "" || len(problems) == 0 {
		t.Fatalf("missing card reported as usable: %+v %v", listed, problems)
	}
	_, finalize, err = Apply(root, "lib", second)
	if err != nil {
		t.Fatal(err)
	}
	finalize()
	if listed, problems = ForInspection(root, "lib", second.Agent); listed.Card == "" || len(problems) != 0 {
		t.Fatalf("recovery failed: %+v %v", listed, problems)
	}

	https, err := Prepare(context.Background(), fetcher, "lib", "g", commitB, ".", manifest.Agent{Card: "https://agents.example/lib"})
	if err != nil {
		t.Fatal(err)
	}
	_, finalize, err = Apply(root, "lib", https)
	if err != nil {
		t.Fatal(err)
	}
	finalize()
	if _, err := os.Stat(filepath.Join(root, ".git-a2a", "agents", "lib")); !os.IsNotExist(err) {
		t.Fatalf("relative to HTTPS transition left stale card: %v", err)
	}
	if listed, problems = ForInspection(root, "lib", https.Agent); listed.Card != https.Agent.Card || len(problems) != 0 {
		t.Fatalf("HTTPS list = %+v %v", listed, problems)
	}
}

func TestPrepareRejectsTraversalBeforeFetch(t *testing.T) {
	fetcher := &recordingFetcher{err: errors.New("must not be called")}
	for _, card := range []string{"../agent.json", "/agent.json", `cards\agent.json`, "."} {
		if _, err := Prepare(context.Background(), fetcher, "lib", "g", strings.Repeat("a", 40), "module", manifest.Agent{Card: card}); err == nil {
			t.Errorf("card %q accepted", card)
		}
	}
	if fetcher.path != "" {
		t.Fatalf("fetch called for unsafe path %q", fetcher.path)
	}
}

func TestApplyAndListRejectSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".git-a2a")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	prepared := Prepared{Agent: manifest.LockedAgent{
		DeclaredCard: "card.json", Card: ResolvedPath("lib"), Commit: strings.Repeat("a", 40),
	}, bytes: []byte("secret")}
	if _, _, err := Apply(root, "lib", prepared); err == nil {
		t.Fatal("symlinked metadata root accepted")
	}
	listed, problems := ForInspection(root, "lib", prepared.Agent)
	if listed.Card != "" || len(problems) == 0 {
		t.Fatalf("symlink exposed as usable: %+v %v", listed, problems)
	}
	if _, err := os.Stat(filepath.Join(outside, "agents", "lib", fileName)); !os.IsNotExist(err) {
		t.Fatalf("wrote outside consumer: %v", err)
	}
}

func TestPrepareReadsByteExactCardFromRequestedGitCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	bare := filepath.Join(tmp, "upstream.git")
	if err := os.MkdirAll(filepath.Join(source, "components", "lib", "cards"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "init", "-b", "main")
	runGit(t, source, "config", "user.email", "test@example.com")
	runGit(t, source, "config", "user.name", "Test")
	runGit(t, source, "config", "core.autocrlf", "false")
	cardPath := filepath.Join(source, "components", "lib", "cards", "owner.json")
	first := []byte("{\r\n  \"version\": 1\r\n}\n")
	if err := os.WriteFile(cardPath, first, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "add", ".")
	runGit(t, source, "commit", "-m", "first")
	commit1 := strings.TrimSpace(runGit(t, source, "rev-parse", "HEAD"))
	second := []byte("{\n  \"version\": 2\n}\n")
	if err := os.WriteFile(cardPath, second, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "commit", "-am", "second")
	commit2 := strings.TrimSpace(runGit(t, source, "rev-parse", "HEAD"))
	runGit(t, tmp, "clone", "--bare", source, bare)

	f := fetch.Fetcher{Runner: gitx.ExecRunner{}}
	root := filepath.Join(tmp, "consumer")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		commit string
		body   []byte
	}{{commit1, first}, {commit2, second}} {
		prepared, err := Prepare(context.Background(), f, "lib", "file://"+bare, want.commit, "components/lib", manifest.Agent{Card: "cards/owner.json"})
		if err != nil {
			t.Fatal(err)
		}
		_, finalize, err := Apply(root, "lib", prepared)
		if err != nil {
			t.Fatal(err)
		}
		finalize()
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(prepared.Agent.Card)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want.body) {
			t.Fatalf("card at %s = %q, want exact %q", want.commit, got, want.body)
		}
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
