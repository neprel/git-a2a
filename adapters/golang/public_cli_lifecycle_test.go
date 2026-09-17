package golang

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	goCLIModule = "github.com/git-a2a-tests/native-go-fixture"
	goCLISource = "https://github.com/git-a2a-tests/native-go-fixture.git"
)

// TestPublicCLIGoLifecycle exercises Go through the public five-command CLI.
// The source is a local Git repository reached through a process-local Git URL
// rewrite, so the Go tool still resolves real pseudo-versions without relying
// on an external forge or module proxy.
func TestPublicCLIGoLifecycle(t *testing.T) {
	if os.Getenv("GITA2A_IT_GO_CLI") != "1" {
		t.Skip("set GITA2A_IT_GO_CLI=1 to exercise the public CLI with the installed Go toolchain")
	}
	for _, tool := range []string{"git", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("required native tool %s: %v", tool, err)
		}
	}

	bin := buildGoCLI(t)
	base := t.TempDir()
	t.Cleanup(func() { makeGoCLITreeWritable(base) })
	upstream := filepath.Join(base, "upstream")
	writeGoCLIFile(t, filepath.Join(upstream, "go.mod"), "module "+goCLIModule+"\n\ngo 1.24\n")
	writeGoCLIFile(t, filepath.Join(upstream, "fixture.go"), "package fixture\n\nfunc Value() string { return \"one\" }\n")
	writeGoCLIFile(t, filepath.Join(upstream, "a2amodule.yml"), "schema: 2\ncomponent:\n  id: native-go\n  exports:\n    - adapter: golang\n      name: "+goCLIModule+"\nagent:\n  card: https://agents.example/native-go.json\n")
	goCLIRun(t, "", nil, "git", "init", "-b", "main", upstream)
	goCLIRun(t, upstream, nil, "git", "config", "user.name", "git-a2a Go native")
	goCLIRun(t, upstream, nil, "git", "config", "user.email", "native-go@example.invalid")
	goCLIRun(t, upstream, nil, "git", "add", ".")
	goCLIRun(t, upstream, nil, "git", "commit", "-m", "one")
	firstCommit := strings.TrimSpace(goCLIRun(t, upstream, nil, "git", "rev-parse", "HEAD"))

	consumer := filepath.Join(base, "consumer")
	writeGoCLIFile(t, filepath.Join(consumer, "go.mod"), "module example.test/consumer\n\ngo 1.24\n\nrequire example.test/unrelated v0.0.0\n\nreplace example.test/unrelated => ./unrelated\n")
	writeGoCLIFile(t, filepath.Join(consumer, "unrelated", "go.mod"), "module example.test/unrelated\n\ngo 1.24\n")
	writeGoCLIFile(t, filepath.Join(consumer, "unrelated", "unrelated.go"), "package unrelated\n\nfunc Value() string { return \"seven\" }\n")
	writeGoCLIConsumerTest(t, consumer, "one:seven", true)
	consumerEnv := goCLIEnv(upstream, filepath.Join(base, "consumer-gomodcache"))
	goCLIRun(t, consumer, consumerEnv, "git", "init", "-b", "main")
	goCLIRun(t, consumer, consumerEnv, "git", "config", "user.name", "git-a2a Go consumer")
	goCLIRun(t, consumer, consumerEnv, "git", "config", "user.email", "consumer-go@example.invalid")

	goCLIRun(t, consumer, consumerEnv, bin, "init", "--id", "native-go-consumer")
	goCLIRun(t, consumer, consumerEnv, bin, "add", goCLISource, "--name", "fixture", "--ref", "main")
	assertGoCLIUse(t, consumer, consumerEnv)
	assertGoCLILockedCommit(t, consumer, firstCommit)

	goCLIRun(t, consumer, consumerEnv, "git", "add", "-A")
	goCLIRun(t, consumer, consumerEnv, "git", "commit", "-m", "installed")

	// A fresh clone has no ignored git-a2a state and uses a fresh module cache.
	fresh := filepath.Join(base, "fresh")
	goCLIRun(t, base, consumerEnv, "git", "clone", consumer, fresh)
	freshEnv := goCLIEnv(upstream, filepath.Join(base, "fresh-gomodcache"))
	goCLIRun(t, fresh, freshEnv, bin, "pull", "fixture")
	assertGoCLIUse(t, fresh, freshEnv)

	// Loss of the external Go cache must be repairable even at an unchanged
	// commit and with unchanged go.mod/go.sum declarations.
	consumerCache := filepath.Join(base, "consumer-gomodcache")
	makeGoCLITreeWritable(consumerCache)
	if err := os.RemoveAll(consumerCache); err != nil {
		t.Fatal(err)
	}
	goCLIRun(t, consumer, consumerEnv, bin, "pull", "fixture")
	assertGoCLIUse(t, consumer, consumerEnv)

	// Advance the branch and pull only this alias. The lock and actual import
	// must move to the new Git commit, not merely to a floating branch name.
	writeGoCLIFile(t, filepath.Join(upstream, "fixture.go"), "package fixture\n\nfunc Value() string { return \"two\" }\n")
	goCLIRun(t, upstream, nil, "git", "commit", "-am", "two")
	secondCommit := strings.TrimSpace(goCLIRun(t, upstream, nil, "git", "rev-parse", "HEAD"))
	writeGoCLIConsumerTest(t, consumer, "two:seven", true)
	goCLIRun(t, consumer, consumerEnv, bin, "pull", "fixture")
	assertGoCLIUse(t, consumer, consumerEnv)
	assertGoCLILockedCommit(t, consumer, secondCommit)

	// Remove only the managed root while retaining and using the unrelated
	// local module. Go checksum evidence is historical/shared and must survive.
	writeGoCLIConsumerTest(t, consumer, "seven", false)
	goSumBefore, err := os.ReadFile(filepath.Join(consumer, "go.sum"))
	if err != nil {
		t.Fatalf("read go.sum before remove: %v", err)
	}
	goCLIRun(t, consumer, consumerEnv, bin, "remove", "fixture")
	assertGoCLIUse(t, consumer, consumerEnv)
	goMod := string(readGoCLIFile(t, filepath.Join(consumer, "go.mod")))
	if strings.Contains(goMod, goCLIModule) || !strings.Contains(goMod, "require example.test/unrelated v0.0.0") || !strings.Contains(goMod, "replace example.test/unrelated => ./unrelated") {
		t.Fatalf("unexpected go.mod after remove:\n%s", goMod)
	}
	if goSumAfter := readGoCLIFile(t, filepath.Join(consumer, "go.sum")); string(goSumAfter) != string(goSumBefore) {
		t.Fatalf("remove rewrote go.sum:\nbefore:\n%s\nafter:\n%s", goSumBefore, goSumAfter)
	}
}

func buildGoCLI(t *testing.T) string {
	t.Helper()
	if binary := os.Getenv("GITA2A_CLI_BIN"); binary != "" {
		info, err := os.Stat(binary)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			t.Fatalf("GITA2A_CLI_BIN %q is not an executable file: %v", binary, err)
		}
		return binary
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	binary := filepath.Join(t.TempDir(), "git-a2a")
	goCLIRun(t, repo, nil, "go", "build", "-o", binary, "./cmd/git-a2a")
	return binary
}

func goCLIEnv(upstream, moduleCache string) []string {
	return []string{
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=url." + goCLIFileURL(upstream) + ".insteadOf",
		"GIT_CONFIG_VALUE_0=" + goCLISource,
		"GIT_CONFIG_KEY_1=url." + goCLIFileURL(upstream) + ".insteadOf",
		"GIT_CONFIG_VALUE_1=" + strings.TrimSuffix(goCLISource, ".git"),
		"GIT_ALLOW_PROTOCOL=file:https",
		"GOPROXY=direct",
		"GOPRIVATE=" + goCLIModule,
		"GONOSUMDB=" + goCLIModule,
		"GOSUMDB=off",
		"GOMODCACHE=" + moduleCache,
	}
}

func goCLIFileURL(path string) string {
	slashed := filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		return "file:///" + strings.TrimPrefix(slashed, "/")
	}
	return "file://" + slashed
}

func writeGoCLIConsumerTest(t *testing.T, root, want string, withFixture bool) {
	t.Helper()
	body := "package consumer_test\n\nimport (\n\t\"testing\"\n\t\"example.test/unrelated\"\n"
	value := "unrelated.Value()"
	if withFixture {
		body += "\tfixture \"" + goCLIModule + "\"\n"
		value = "fixture.Value() + \":\" + unrelated.Value()"
	}
	body += ")\n\nfunc TestImports(t *testing.T) {\n\tif got := " + value + "; got != \"" + want + "\" {\n\t\tt.Fatalf(\"value = %q\", got)\n\t}\n}\n"
	writeGoCLIFile(t, filepath.Join(root, "consumer_test.go"), body)
}

func assertGoCLIUse(t *testing.T, root string, env []string) {
	t.Helper()
	goCLIRun(t, root, env, "go", "test", "-mod=readonly", "./...")
}

func assertGoCLILockedCommit(t *testing.T, root, commit string) {
	t.Helper()
	body := string(readGoCLIFile(t, filepath.Join(root, "a2amodule.lock")))
	if !strings.Contains(body, commit) {
		t.Fatalf("lock does not contain commit %s:\n%s", commit, body)
	}
	goMod := string(readGoCLIFile(t, filepath.Join(root, "go.mod")))
	if !strings.Contains(goMod, commit[:12]) {
		t.Fatalf("go.mod does not pin commit prefix %s:\n%s", commit[:12], goMod)
	}
}

func writeGoCLIFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readGoCLIFile(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func makeGoCLITreeWritable(root string) {
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil {
			_ = os.Chmod(path, 0o700)
		}
		return nil
	})
}

func goCLIRun(t *testing.T, dir string, extraEnv []string, command string, args ...string) string {
	t.Helper()
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0"), extraEnv...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", command, strings.Join(args, " "), err, output)
	}
	return string(output)
}
