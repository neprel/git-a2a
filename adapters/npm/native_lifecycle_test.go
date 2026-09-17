package npm

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/neprel/git-a2a/internal/adapter"
)

// TestNativeNPMVariantsLifecycle is intentionally opt-in: a native job that
// advertises this matrix sets GITA2A_IT_NPM=1 and therefore fails (rather than
// silently skipping) when any selected manager is missing or broken.
func TestNativeNPMVariantsLifecycle(t *testing.T) {
	if os.Getenv("GITA2A_IT_NPM") != "1" {
		t.Skip("set GITA2A_IT_NPM=1 to exercise npm, Yarn Berry, pnpm, and Bun")
	}
	for _, tc := range []struct {
		name, manager string
		prepare       func(*testing.T, string, string)
	}{
		{name: "npm", manager: "npm@11.18.0"},
		{name: "yarn-berry", manager: "yarn@4.17.0", prepare: func(t *testing.T, root, source string) {
			writeNativeFile(t, filepath.Join(root, ".yarnrc.yml"), "nodeLinker: node-modules\napprovedGitRepositories:\n  - '"+source+"'\n")
		}},
		{name: "pnpm", manager: "pnpm@9.7.0"},
		{name: "bun", manager: "bun@1.4.2"},
	} {
		t.Run(tc.name, func(t *testing.T) { exerciseNPMVariant(t, tc.manager, tc.prepare) })
	}
}

func exerciseNPMVariant(t *testing.T, packageManager string, prepare func(*testing.T, string, string)) {
	t.Helper()
	fixture := newNPMGitFixture(t)
	unrelated := filepath.Join(t.TempDir(), "unrelated")
	writeNativeFile(t, filepath.Join(unrelated, "package.json"), `{"name":"unrelated","version":"7.0.0","main":"index.js"}`+"\n")
	writeNativeFile(t, filepath.Join(unrelated, "index.js"), "module.exports = { value: 7 };\n")

	root := t.TempDir()
	document := map[string]any{
		"name":           "native-consumer",
		"private":        true,
		"packageManager": packageManager,
		"dependencies":   map[string]string{"unrelated": "file:" + filepath.ToSlash(unrelated)},
	}
	body, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeNativeFile(t, filepath.Join(root, "package.json"), string(body)+"\n")
	if prepare != nil {
		prepare(t, root, fixture.url)
	}

	dep := adapter.Dependency{Name: "native-lib", Git: fixture.url, Ref: "main"}
	exp := adapter.Export{Adapter: "npm", Name: "@native/lib"}
	locked := adapter.Locked{Git: fixture.url, Commit: fixture.first}
	a := Adapter{}
	if err := a.Capability(root, dep, exp); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Pull(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatalf("initial Pull: %v", err)
	}
	assertNodeValue(t, root, "@native/lib", "one")
	assertNodeValue(t, root, "unrelated", "7")
	assertNPMInspectClean(t, a, root, dep, exp, locked)

	// A fresh consumer checkout has declarations and native lock but no local
	// materialization. Pull must recreate it without changing the selected variant.
	fresh := t.TempDir()
	copyNPMState(t, root, fresh)
	if _, err := a.Pull(context.Background(), fresh, dep, exp, locked); err != nil {
		t.Fatalf("fresh Pull: %v", err)
	}
	assertNodeValue(t, fresh, "@native/lib", "one")
	assertNodeValue(t, fresh, "unrelated", "7")

	if err := os.RemoveAll(filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Pull(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatalf("repair Pull: %v", err)
	}
	assertNodeValue(t, root, "@native/lib", "one")

	second := fixture.commit(t, "two")
	next := locked
	next.Commit = second
	if _, err := a.Pull(context.Background(), root, dep, exp, next); err != nil {
		t.Fatalf("advance Pull: %v", err)
	}
	assertNodeValue(t, root, "@native/lib", "two")
	assertNodeValue(t, root, "unrelated", "7")
	assertNPMInspectClean(t, a, root, dep, exp, next)

	if _, err := a.Remove(context.Background(), root, dep, exp, next); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	assertNodeMissing(t, root, "@native/lib")
	assertNodeValue(t, root, "unrelated", "7")
	manifest := string(readNativeFile(t, filepath.Join(root, "package.json")))
	if strings.Contains(manifest, `"@native/lib"`) {
		t.Fatalf("root declaration remains:\n%s", manifest)
	}
	lockPath, _, _ := npmLockedRevision(root, detectedNPMVariant(t, root), exp.Name, next.Commit)
	if lockBody, err := os.ReadFile(filepath.Join(root, lockPath)); err == nil && strings.Contains(string(lockBody), exp.Name) {
		t.Fatalf("native lock %s retains removed root package", lockPath)
	}
}

type npmGitFixture struct {
	t     *testing.T
	root  string
	url   string
	first string
}

func newNPMGitFixture(t *testing.T) *npmGitFixture {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "native-lib")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	runNative(t, "", "git", "init", "-b", "main", root)
	runNative(t, root, "git", "config", "user.name", "git-a2a native")
	runNative(t, root, "git", "config", "user.email", "native@example.invalid")
	writeNativeFile(t, filepath.Join(root, "package.json"), `{"name":"@native/lib","version":"1.0.0","main":"index.js"}`+"\n")
	writeNativeFile(t, filepath.Join(root, "index.js"), "module.exports = { value: 'one' };\n")
	runNative(t, root, "git", "add", ".")
	runNative(t, root, "git", "commit", "-m", "one")
	first := strings.TrimSpace(runNative(t, root, "git", "rev-parse", "HEAD"))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	cmd := exec.Command("git", "daemon", "--reuseaddr", "--export-all", "--listen=127.0.0.1", fmt.Sprintf("--port=%d", port), "--base-path="+base, base)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	address := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(5 * time.Second)
	for {
		connection, dialErr := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if dialErr == nil {
			_ = connection.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("git daemon did not start: %v", dialErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return &npmGitFixture{t: t, root: root, url: "git://" + address + "/native-lib", first: first}
}

func (f *npmGitFixture) commit(t *testing.T, value string) string {
	t.Helper()
	writeNativeFile(t, filepath.Join(f.root, "index.js"), "module.exports = { value: '"+value+"' };\n")
	runNative(t, f.root, "git", "commit", "-am", value)
	return strings.TrimSpace(runNative(t, f.root, "git", "rev-parse", "HEAD"))
}

func detectedNPMVariant(t *testing.T, root string) adapter.Variant {
	t.Helper()
	ok, variant, err := (Adapter{}).Detect(root)
	if err != nil || !ok {
		t.Fatalf("Detect: ok=%v variant=%s err=%v", ok, variant, err)
	}
	return variant
}

func assertNPMInspectClean(t *testing.T, a Adapter, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) {
	t.Helper()
	findings, err := a.Inspect(context.Background(), root, dep, exp, locked)
	if err != nil || len(findings) != 0 {
		t.Fatalf("Inspect: findings=%v err=%v", findings, err)
	}
}

func assertNodeValue(t *testing.T, root, name, want string) {
	t.Helper()
	got := strings.TrimSpace(runNative(t, root, "node", "-e", "process.stdout.write(String(require("+strconvQuote(name)+").value))"))
	if got != want {
		t.Fatalf("require(%s).value=%q, want %q", name, got, want)
	}
}

func assertNodeMissing(t *testing.T, root, name string) {
	t.Helper()
	cmd := exec.Command("node", "-e", "require("+strconvQuote(name)+")")
	cmd.Dir = root
	if err := cmd.Run(); err == nil {
		t.Fatalf("removed package %s remains requireable", name)
	}
}

func strconvQuote(value string) string {
	body, _ := json.Marshal(value)
	return string(body)
}

func copyNPMState(t *testing.T, from, to string) {
	t.Helper()
	for _, name := range []string{"package.json", ".yarnrc.yml", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "bun.lock", "bun.lockb"} {
		body, err := os.ReadFile(filepath.Join(from, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		writeNativeFile(t, filepath.Join(to, name), string(body))
	}
}

func writeNativeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readNativeFile(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func runNative(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}
