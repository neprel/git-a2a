package nix

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPublicCLINixLifecycle runs only in the pinned native environment. That
// environment advertises Nix support, so a missing tool is a failure, not a skip.
func TestPublicCLINixLifecycle(t *testing.T) {
	if os.Getenv("GITA2A_IT_NIX") != "1" {
		t.Skip("set GITA2A_IT_NIX=1 in the pinned Nix environment")
	}
	for _, tool := range []string{"git", "nix"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("required native tool %s: %v", tool, err)
		}
	}
	t.Log(strings.TrimSpace(runNativeNix(t, "", "nix", "--version")))
	t.Log(strings.TrimSpace(runNativeNix(t, "", "git", "--version")))
	t.Logf("runner=%s/%s", runtime.GOOS, runtime.GOARCH)
	bin := os.Getenv("GITA2A_CLI_BIN")
	if bin == "" {
		t.Fatal("GITA2A_CLI_BIN must point to the Linux public CLI binary")
	}
	if info, err := os.Stat(bin); err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		t.Fatalf("GITA2A_CLI_BIN must name an executable file: %s: %v", bin, err)
	}

	base := t.TempDir()
	upstream := filepath.Join(base, "fixture")
	gitInitNativeNix(t, upstream)
	writeNativeNix(t, upstream, "a2amodule.yml", "schema: 2\ncomponent:\n  id: fixture-nix\n  exports:\n    - adapter: nix\n      name: fixture\nagent:\n  card: https://agents.example/fixture-nix.json\n")
	writeUpstreamFlake(t, upstream, "one")
	gitCommitNativeNix(t, upstream, "one")

	consumer := filepath.Join(base, "consumer")
	gitInitNativeNix(t, consumer)
	writeNativeNix(t, consumer, "flake.nix", consumerFlake)
	writeNativeNix(t, consumer, "unrelated/flake.nix", `{ outputs = { self }: { lib.value = "seven"; }; }`+"\n")
	writeNativeNix(t, consumer, "unrelated-marker", "preserve exactly\n")
	gitCommitNativeNix(t, consumer, "consumer baseline")

	runNativeNix(t, consumer, bin, "init", "--id", "consumer")
	runNativeNix(t, consumer, bin, "add", fileURLNativeNix(upstream), "--name", "fixture", "--ref", "main")
	assertNixValue(t, consumer, "fixture", "one")
	assertNixValue(t, consumer, "unrelated", "seven")
	assertNixLockRevision(t, consumer, "fixture", gitRevisionNativeNix(t, upstream))
	listSnapshot := map[string][]byte{}
	for _, name := range []string{"a2amodule.yml", "a2amodule.lock", "flake.nix", "flake.lock"} {
		listSnapshot[name] = readNativeNix(t, filepath.Join(consumer, name))
	}
	if output := runNativeNix(t, consumer, bin, "list"); !strings.Contains(output, "fixture") {
		t.Fatalf("public list omitted fixture:\n%s", output)
	}
	for name, body := range listSnapshot {
		assertBytesNativeNix(t, filepath.Join(consumer, name), body)
	}
	gitCommitNativeNix(t, consumer, "installed")

	// A fresh checkout cannot rely on the first checkout's Nix store state.
	fresh := filepath.Join(base, "fresh")
	runNativeNix(t, base, "git", "clone", consumer, fresh)
	runNativeNix(t, fresh, bin, "pull", "fixture")
	assertNixValue(t, fresh, "fixture", "one")
	assertNixValue(t, fresh, "unrelated", "seven")

	// Repair the manager lock without changing the selected Git revision.
	if err := os.Remove(filepath.Join(consumer, "flake.lock")); err != nil {
		t.Fatal(err)
	}
	runNativeNix(t, consumer, bin, "pull", "fixture")
	assertNixValue(t, consumer, "fixture", "one")
	assertNixLockRevision(t, consumer, "fixture", gitRevisionNativeNix(t, upstream))

	// A native-manager failure must restore all owned files and the usable revision.
	before := map[string][]byte{}
	for _, name := range []string{"a2amodule.yml", "a2amodule.lock", "flake.lock", "flake.nix"} {
		before[name] = readNativeNix(t, filepath.Join(consumer, name))
	}
	writeNativeNix(t, upstream, "flake.nix", "{ this is not valid nix; }\n")
	gitCommitNativeNix(t, upstream, "broken")
	runNativeNixFails(t, consumer, bin, "pull", "fixture")
	for name, body := range before {
		assertBytesNativeNix(t, filepath.Join(consumer, name), body)
	}
	assertNixValue(t, consumer, "fixture", "one")

	// Advance the same branch, using targeted pull, without disturbing unrelated.
	writeUpstreamFlake(t, upstream, "two")
	gitCommitNativeNix(t, upstream, "two")
	second := gitRevisionNativeNix(t, upstream)
	runNativeNix(t, consumer, bin, "pull", "fixture")
	assertNixValue(t, consumer, "fixture", "two")
	assertNixValue(t, consumer, "unrelated", "seven")
	assertNixLockRevision(t, consumer, "fixture", second)
	gitCommitNativeNix(t, consumer, "updated")
	runNativeNix(t, consumer, bin, "pull", "fixture")
	if got := strings.TrimSpace(runNativeNix(t, consumer, "git", "status", "--porcelain")); got != "" {
		t.Fatalf("idempotent targeted pull left a diff: %s", got)
	}

	// outputs is user-owned Nix code. Stop referring to fixture before asking
	// the adapter to remove only its owned input declaration.
	flake := string(readNativeNix(t, filepath.Join(consumer, "flake.nix")))
	oldOutputs := `  outputs = { self, unrelated, fixture ? null }: {
    lib.fixture = if fixture == null then "absent" else fixture.lib.value;
    lib.unrelated = unrelated.lib.value;
  };`
	newOutputs := `  outputs = { self, unrelated }: {
    lib.unrelated = unrelated.lib.value;
  };`
	if !strings.Contains(flake, oldOutputs) {
		t.Fatalf("consumer outputs changed unexpectedly:\n%s", flake)
	}
	writeNativeNix(t, consumer, "flake.nix", strings.Replace(flake, oldOutputs, newOutputs, 1))
	runNativeNix(t, consumer, bin, "remove", "fixture")
	assertNixValue(t, consumer, "unrelated", "seven")
	if got := readNativeNix(t, filepath.Join(consumer, "flake.nix")); strings.Contains(string(got), "git-a2a:begin fixture") {
		t.Fatalf("removed managed input remains:\n%s", got)
	}
	assertNixLockMissingInput(t, consumer, "fixture")
	if got := string(readNativeNix(t, filepath.Join(consumer, "unrelated-marker"))); got != "preserve exactly\n" {
		t.Fatalf("unrelated file changed: %q", got)
	}
}

const consumerFlake = `{
  inputs.unrelated.url = "path:./unrelated";
  outputs = { self, unrelated, fixture ? null }: {
    lib.fixture = if fixture == null then "absent" else fixture.lib.value;
    lib.unrelated = unrelated.lib.value;
  };
}
`

func writeUpstreamFlake(t *testing.T, root, value string) {
	t.Helper()
	writeNativeNix(t, root, "flake.nix", `{ outputs = { self }: { lib.value = "`+value+`"; }; }`+"\n")
}

func assertNixValue(t *testing.T, root, attribute, want string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(runNativeNix(t, root, "nix", "eval", "--raw", ".#lib."+attribute)), "\n")
	got := strings.TrimSpace(lines[len(lines)-1])
	if got != want {
		t.Fatalf("nix value %s=%q, want %q", attribute, got, want)
	}
}

type nativeNixLock struct {
	Nodes map[string]struct {
		Inputs map[string]any `json:"inputs"`
		Locked struct {
			Rev string `json:"rev"`
		} `json:"locked"`
	} `json:"nodes"`
	Root string `json:"root"`
}

func readNixLock(t *testing.T, root string) nativeNixLock {
	t.Helper()
	var lock nativeNixLock
	if err := json.Unmarshal(readNativeNix(t, filepath.Join(root, "flake.lock")), &lock); err != nil {
		t.Fatalf("parse flake.lock: %v", err)
	}
	return lock
}

func assertNixLockRevision(t *testing.T, root, input, want string) {
	t.Helper()
	lock := readNixLock(t, root)
	nodeName, ok := lock.Nodes[lock.Root].Inputs[input].(string)
	if !ok {
		t.Fatalf("flake.lock root input %q is %#v", input, lock.Nodes[lock.Root].Inputs[input])
	}
	if got := lock.Nodes[nodeName].Locked.Rev; got != want {
		t.Fatalf("flake.lock revision=%q, want %q", got, want)
	}
}

func assertNixLockMissingInput(t *testing.T, root, input string) {
	t.Helper()
	lock := readNixLock(t, root)
	if _, ok := lock.Nodes[lock.Root].Inputs[input]; ok {
		t.Fatalf("flake.lock retained removed root input %q", input)
	}
}

func gitInitNativeNix(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	runNativeNix(t, root, "git", "init", "-b", "main")
	runNativeNix(t, root, "git", "config", "user.email", "native@example.test")
	runNativeNix(t, root, "git", "config", "user.name", "Nix Native")
}

func gitCommitNativeNix(t *testing.T, root, message string) {
	t.Helper()
	runNativeNix(t, root, "git", "add", "-A")
	runNativeNix(t, root, "git", "commit", "-m", message)
}

func gitRevisionNativeNix(t *testing.T, root string) string {
	t.Helper()
	return strings.TrimSpace(runNativeNix(t, root, "git", "rev-parse", "HEAD"))
}

func fileURLNativeNix(path string) string {
	path = filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		return "file:///" + strings.TrimPrefix(path, "/")
	}
	return "file://" + path
}

func writeNativeNix(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readNativeNix(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func assertBytesNativeNix(t *testing.T, path string, want []byte) {
	t.Helper()
	if got := readNativeNix(t, path); string(got) != string(want) {
		t.Fatalf("%s changed across failed manager operation\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}

func runNativeNix(t *testing.T, dir, command string, args ...string) string {
	t.Helper()
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_ALLOW_PROTOCOL=file", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", command, strings.Join(args, " "), err, out)
	}
	return string(out)
}

func runNativeNixFails(t *testing.T, dir, command string, args ...string) {
	t.Helper()
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_ALLOW_PROTOCOL=file", "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("%s %s unexpectedly succeeded\n%s", command, strings.Join(args, " "), out)
	}
}
