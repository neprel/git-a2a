package gitx

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Runner interface {
	Run(ctx context.Context, dir string, stdin []byte, args ...string) ([]byte, error)
}

type ExecRunner struct{ Timeout time.Duration }
type Resolution struct {
	Commit, FullRef, Kind string
	Ambiguous             bool
}

func (r ExecRunner) Run(ctx context.Context, dir string, stdin []byte, args ...string) ([]byte, error) {
	if r.Timeout <= 0 {
		r.Timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("git %s: %w", args[0], ctx.Err())
		}
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func Resolve(ctx context.Context, runner Runner, url, ref string) (string, error) {
	r, err := ResolveDetailed(ctx, runner, url, ref)
	return r.Commit, err
}

func ResolveDetailed(ctx context.Context, runner Runner, url, ref string) (Resolution, error) {
	if isCommit(ref) {
		return Resolution{Commit: strings.ToLower(ref), FullRef: ref, Kind: "pinned"}, nil
	}
	query := ref
	if query == "" || query == "HEAD" {
		out, err := runner.Run(ctx, "", nil, "ls-remote", url, "HEAD")
		if err != nil {
			return Resolution{}, err
		}
		values := parseRemote(out)
		if commit := values["HEAD"]; commit != "" {
			return Resolution{Commit: commit, FullRef: "HEAD", Kind: "head"}, nil
		}
		return Resolution{}, fmt.Errorf("remote HEAD was not found at %s", url)
	}
	if strings.HasPrefix(query, "refs/") {
		patterns := []string{query}
		if strings.HasPrefix(query, "refs/tags/") {
			patterns = append(patterns, query+"^{}")
		}
		out, err := runner.Run(ctx, "", nil, append([]string{"ls-remote", url}, patterns...)...)
		if err != nil {
			return Resolution{}, err
		}
		values := parseRemote(out)
		commit := values[query+"^{}"]
		if commit == "" {
			commit = values[query]
		}
		if commit == "" {
			return Resolution{}, fmt.Errorf("ref %q was not found at %s", query, url)
		}
		kind := "ref"
		if strings.HasPrefix(query, "refs/tags/") {
			kind = "tag"
		} else if strings.HasPrefix(query, "refs/heads/") {
			kind = "branch"
		}
		return Resolution{Commit: commit, FullRef: query, Kind: kind}, nil
	}
	tag := "refs/tags/" + query
	head := "refs/heads/" + query
	out, err := runner.Run(ctx, "", nil, "ls-remote", url, tag, tag+"^{}", head)
	if err != nil {
		return Resolution{}, err
	}
	values := parseRemote(out)
	tagCommit := values[tag+"^{}"]
	if tagCommit == "" {
		tagCommit = values[tag]
	}
	headCommit := values[head]
	if tagCommit != "" {
		return Resolution{Commit: tagCommit, FullRef: tag, Kind: "tag", Ambiguous: headCommit != ""}, nil
	}
	if headCommit != "" {
		return Resolution{Commit: headCommit, FullRef: head, Kind: "branch"}, nil
	}
	return Resolution{}, fmt.Errorf("ref %q was not found at %s", query, url)
}

func parseRemote(out []byte) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && isCommit(fields[0]) {
			values[fields[1]] = strings.ToLower(fields[0])
		}
	}
	return values
}
func isCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, c := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}
func NormalizeURL(raw string) string {
	s := strings.TrimSpace(strings.TrimPrefix(raw, "git+"))
	if strings.HasPrefix(s, "git@") {
		parts := strings.SplitN(strings.TrimPrefix(s, "git@"), ":", 2)
		if len(parts) == 2 {
			s = parts[0] + "/" + parts[1]
		}
	} else if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
		s = strings.TrimPrefix(s, "git@")
	} else {
		s = strings.TrimPrefix(s, "ssh://")
	}
	s = strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
	return strings.ToLower(s)
}
