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
	query := ref
	if query == "" || query == "HEAD" {
		query = "HEAD"
	}
	out, err := runner.Run(ctx, "", nil, "ls-remote", url, query, "refs/heads/"+query, "refs/tags/"+query, "refs/tags/"+query+"^{}")
	if err != nil {
		return "", err
	}
	lines := strings.Fields(string(out))
	for i := len(lines) - 2; i >= 0; i -= 2 {
		if len(lines[i]) == 40 {
			return strings.ToLower(lines[i]), nil
		}
	}
	if len(ref) == 40 {
		for _, c := range ref {
			if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
				return "", fmt.Errorf("ref %q was not found", ref)
			}
		}
		return strings.ToLower(ref), nil
	}
	return "", fmt.Errorf("ref %q was not found at %s", query, url)
}
