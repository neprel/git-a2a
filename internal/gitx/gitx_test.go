package gitx

import (
	"context"
	"strings"
	"testing"
)

type resolveRunner struct {
	out  string
	args []string
}

func (r *resolveRunner) Run(_ context.Context, _ string, _ []byte, args ...string) ([]byte, error) {
	r.args = args
	return []byte(r.out), nil
}
func TestResolveShortNamePrefersTagAndReportsAmbiguity(t *testing.T) {
	tag := strings.Repeat("a", 40)
	branch := strings.Repeat("b", 40)
	r := &resolveRunner{out: tag + "\trefs/tags/release\n" + branch + "\trefs/heads/release\n"}
	got, err := ResolveDetailed(context.Background(), r, "file:///repo", "release")
	if err != nil {
		t.Fatal(err)
	}
	if got.Commit != tag || got.FullRef != "refs/tags/release" || got.Kind != "tag" || !got.Ambiguous {
		t.Fatalf("got %#v", got)
	}
}
func TestResolveFullBranchAndCommit(t *testing.T) {
	sha := strings.Repeat("c", 40)
	r := &resolveRunner{out: sha + "\trefs/heads/main\n"}
	got, err := ResolveDetailed(context.Background(), r, "file:///repo", "refs/heads/main")
	if err != nil || got.Commit != sha || got.Kind != "branch" {
		t.Fatalf("got %#v %v", got, err)
	}
	got, err = ResolveDetailed(context.Background(), r, "file:///repo", sha)
	if err != nil || got.Kind != "pinned" {
		t.Fatalf("got %#v %v", got, err)
	}
}
func TestResolveHeadReturnsSymbolicDefaultBranch(t *testing.T) {
	sha := strings.Repeat("d", 40)
	r := &resolveRunner{out: "ref: refs/heads/trunk\tHEAD\n" + sha + "\tHEAD\n"}
	got, err := ResolveDetailed(context.Background(), r, "file:///repo", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if got.Commit != sha || got.FullRef != "refs/heads/trunk" || got.Kind != "branch" {
		t.Fatalf("got %#v", got)
	}
	if strings.Join(r.args, " ") != "ls-remote --symref file:///repo HEAD" {
		t.Fatalf("args = %v", r.args)
	}
}
func TestNormalizeURL(t *testing.T) {
	values := []string{"git@github.com:Acme/lib.git", "ssh://git@github.com/Acme/lib.git", "git+https://github.com/Acme/lib"}
	for _, value := range values {
		if got := NormalizeURL(value); got != "github.com/acme/lib" {
			t.Errorf("%s -> %s", value, got)
		}
	}
}
