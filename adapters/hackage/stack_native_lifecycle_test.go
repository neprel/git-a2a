package hackage

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestPublicCLIStackLifecycle is run by the central native-lifecycle runner in a pinned Linux
// toolchain. Once enabled, an absent CLI, Git, GHC, or Stack is a test failure:
// a native evidence job must never turn a missing advertised tool into a skip.
func TestPublicCLIStackLifecycle(t *testing.T) {
	if os.Getenv("GITA2A_NATIVE_HACKAGE_IT") != "stack" {
		t.Skip("run the stack case through tools/native-lifecycle/run.py")
	}
	bin := os.Getenv("GITA2A_CLI_BIN")
	if bin == "" {
		t.Fatal("GITA2A_CLI_BIN must point to the Linux public CLI")
	}
	for _, tool := range []string{bin, "git", "ghc", "stack"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("required native tool %q: %v", tool, err)
		}
	}

	native := newStackNativeUpstream(t, "native-lib", "NativeLib", "nativeValue", "native-one")
	held := newStackNativeUpstream(t, "held-lib", "HeldLib", "heldValue", "held-one")
	consumer := newStackNativeConsumer(t)

	stackNativeRun(t, consumer, bin, "init", "--id", "stack-consumer")
	stackNativeRun(t, consumer, bin, "add", stackNativeFileURL(native.root), "--name", "native", "--ref", "main")
	assertStackNativeOutput(t, consumer, "native-one:unrelated")
	assertStackNativeEvidence(t, consumer, "native")

	// A second git-a2a dependency proves that a targeted pull does not advance
	// any other saved binding. Add it only after the first build is usable.
	writeStackNativeConsumer(t, consumer, true, true)
	stackNativeRun(t, consumer, bin, "add", stackNativeFileURL(held.root), "--name", "held", "--ref", "main")
	assertStackNativeOutput(t, consumer, "native-one:held-one:unrelated")
	assertStackNativeEvidence(t, consumer, "native")
	assertStackNativeEvidence(t, consumer, "held")

	stackNativeGit(t, consumer, "init", "-b", "main")
	stackNativeGit(t, consumer, "config", "user.email", "native@example.invalid")
	stackNativeGit(t, consumer, "config", "user.name", "git-a2a Stack native")
	stackNativeGit(t, consumer, "add", "a2amodule.yml", "a2amodule.lock", ".gitignore", "stack.yaml", "stack.yaml.lock", "stack-consumer.cabal", "app", "unrelated")
	stackNativeGit(t, consumer, "commit", "-m", "installed Stack dependencies")

	// A clone contains no .stack-work. Public Pull must recreate usable local
	// materialization without any manual Stack command between clone and use.
	fresh := filepath.Join(t.TempDir(), "fresh")
	stackNativeRun(t, "", "git", "clone", consumer, fresh)
	stackNativeRun(t, fresh, bin, "pull", "native")
	assertStackNativeOutput(t, fresh, "native-one:held-one:unrelated")
	assertStackNativeEvidence(t, fresh, "native")

	if err := os.RemoveAll(filepath.Join(consumer, ".stack-work")); err != nil {
		t.Fatal(err)
	}
	stackNativeRun(t, consumer, bin, "pull", "native")
	assertStackNativeOutput(t, consumer, "native-one:held-one:unrelated")
	assertStackNativeEvidence(t, consumer, "native")

	beforeHeld := stackNativeLockedCommit(t, consumer, "held")
	native.advance(t, "native-two")
	held.advance(t, "held-two")
	stackNativeRun(t, consumer, bin, "pull", "native")
	assertStackNativeOutput(t, consumer, "native-two:held-one:unrelated")
	assertStackNativeEvidence(t, consumer, "native")
	if got := stackNativeLockedCommit(t, consumer, "held"); got != beforeHeld {
		t.Fatalf("targeted pull advanced held dependency: got %s, want %s", got, beforeHeld)
	}

	// Reapplying the same commit may compile, but must not churn declaration or
	// native lock bytes.
	stackYAMLBefore := stackNativeRead(t, filepath.Join(consumer, "stack.yaml"))
	stackLockBefore := stackNativeRead(t, filepath.Join(consumer, "stack.yaml.lock"))
	stackNativeRun(t, consumer, bin, "pull", "native")
	if got := stackNativeRead(t, filepath.Join(consumer, "stack.yaml")); got != stackYAMLBefore {
		t.Fatal("idempotent targeted pull changed stack.yaml")
	}
	if got := stackNativeRead(t, filepath.Join(consumer, "stack.yaml.lock")); got != stackLockBefore {
		t.Fatal("idempotent targeted pull changed stack.yaml.lock")
	}

	// Stop consumer code from requesting the dependency before its root entry
	// is removed. The unrelated package and held git dependency must still build.
	writeStackNativeConsumer(t, consumer, false, true)
	stackNativeRun(t, consumer, bin, "remove", "native")
	assertStackNativeRunOutput(t, consumer, "held-one:unrelated")
	stackYAML := stackNativeRead(t, filepath.Join(consumer, "stack.yaml"))
	stackLock := stackNativeRead(t, filepath.Join(consumer, "stack.yaml.lock"))
	if strings.Contains(stackYAML, stackNativeFileURL(native.root)) || strings.Contains(stackLock, stackNativeFileURL(native.root)) {
		t.Fatal("removed Stack dependency remains in declaration or native lock")
	}
	if !strings.Contains(stackYAML, stackNativeFileURL(held.root)) || !strings.Contains(stackLock, stackNativeFileURL(held.root)) {
		t.Fatal("remove damaged unrelated git-a2a dependency")
	}
	if !strings.Contains(stackYAML, "unrelated") {
		t.Fatal("remove damaged unrelated project package")
	}
}

type stackNativeUpstream struct {
	root, packageName, module, function string
}

func newStackNativeUpstream(t *testing.T, packageName, module, function, value string) stackNativeUpstream {
	t.Helper()
	f := stackNativeUpstream{root: t.TempDir(), packageName: packageName, module: module, function: function}
	stackNativeGit(t, f.root, "init", "-b", "main")
	stackNativeGit(t, f.root, "config", "user.email", "native@example.invalid")
	stackNativeGit(t, f.root, "config", "user.name", "git-a2a Stack native")
	stackNativeWrite(t, filepath.Join(f.root, "a2amodule.yml"), "schema: 2\ncomponent:\n  id: "+packageName+"\n  exports:\n    - adapter: hackage\n      name: "+packageName+"\nagent:\n  card: https://agents.example/"+packageName+".json\n")
	stackNativeWrite(t, filepath.Join(f.root, packageName+".cabal"), "cabal-version: 2.4\nname: "+packageName+"\nversion: 0.1.0.0\nbuild-type: Simple\nlibrary\n  exposed-modules: "+module+"\n  hs-source-dirs: src\n  build-depends: base >=4.15 && <5\n  default-language: Haskell2010\n")
	f.writeValue(t, value)
	stackNativeGit(t, f.root, "add", ".")
	stackNativeGit(t, f.root, "commit", "-m", "initial same-version package")
	return f
}

func (f stackNativeUpstream) writeValue(t *testing.T, value string) {
	stackNativeWrite(t, filepath.Join(f.root, "src", f.module+".hs"), "module "+f.module+" ("+f.function+") where\n\n"+f.function+" :: String\n"+f.function+" = "+stackNativeQuote(value)+"\n")
}

func (f stackNativeUpstream) advance(t *testing.T, value string) {
	t.Helper()
	f.writeValue(t, value)
	stackNativeGit(t, f.root, "add", ".")
	stackNativeGit(t, f.root, "commit", "-m", value+" at unchanged package version")
}

func newStackNativeConsumer(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	stackNativeWrite(t, filepath.Join(root, "stack.yaml"), "resolver: ghc-9.0.2\nsystem-ghc: true\ninstall-ghc: false\npackages:\n- .\n- unrelated\n")
	stackNativeWrite(t, filepath.Join(root, "unrelated", "unrelated-lib.cabal"), "cabal-version: 2.4\nname: unrelated-lib\nversion: 7.0.0.0\nbuild-type: Simple\nlibrary\n  exposed-modules: Unrelated\n  hs-source-dirs: src\n  build-depends: base >=4.15 && <5\n  default-language: Haskell2010\n")
	stackNativeWrite(t, filepath.Join(root, "unrelated", "src", "Unrelated.hs"), "module Unrelated (unrelatedValue) where\nunrelatedValue :: String\nunrelatedValue = \"unrelated\"\n")
	writeStackNativeConsumer(t, root, true, false)
	return root
}

func writeStackNativeConsumer(t *testing.T, root string, withNative, withHeld bool) {
	t.Helper()
	deps := []string{"base >=4.15 && <5", "unrelated-lib"}
	imports := []string{"import Unrelated (unrelatedValue)"}
	values := []string{}
	if withNative {
		deps = append(deps, "native-lib")
		imports = append(imports, "import NativeLib (nativeValue)")
		values = append(values, "nativeValue")
	}
	if withHeld {
		deps = append(deps, "held-lib")
		imports = append(imports, "import HeldLib (heldValue)")
		values = append(values, "heldValue")
	}
	values = append(values, "unrelatedValue")
	stackNativeWrite(t, filepath.Join(root, "stack-consumer.cabal"), "cabal-version: 2.4\nname: stack-consumer\nversion: 0.1.0.0\nbuild-type: Simple\nexecutable stack-consumer\n  main-is: Main.hs\n  hs-source-dirs: app\n  build-depends:\n    "+strings.Join(deps, "\n    , ")+"\n  default-language: Haskell2010\n")
	stackNativeWrite(t, filepath.Join(root, "app", "Main.hs"), "module Main where\n"+strings.Join(imports, "\n")+"\nmain :: IO ()\nmain = putStrLn ("+strings.Join(values, " ++ \":\" ++ ")+")\n")
}

func assertStackNativeOutput(t *testing.T, root, want string) {
	t.Helper()
	if got := strings.TrimSpace(stackNativeRun(t, root, "stack", "exec", "stack-consumer")); got != want {
		t.Fatalf("consumer output = %q, want %q", got, want)
	}
}

func assertStackNativeRunOutput(t *testing.T, root, want string) {
	t.Helper()
	output := strings.TrimSpace(stackNativeRun(t, root, "stack", "run", "stack-consumer"))
	lines := strings.Split(output, "\n")
	if got := strings.TrimSpace(lines[len(lines)-1]); got != want {
		t.Fatalf("post-remove consumer output = %q, want %q", got, want)
	}
}

func stackNativeLockedCommit(t *testing.T, root, name string) string {
	t.Helper()
	body := stackNativeRead(t, filepath.Join(root, "a2amodule.lock"))
	var document struct {
		Dependencies map[string]struct {
			Commit string `yaml:"commit"`
		} `yaml:"dependencies"`
	}
	if err := yaml.Unmarshal([]byte(body), &document); err != nil {
		t.Fatalf("decode lock: %v", err)
	}
	dependency, ok := document.Dependencies[name]
	if !ok {
		t.Fatalf("lock has no %s dependency:\n%s", name, body)
	}
	if dependency.Commit == "" {
		t.Fatalf("lock dependency %s has no commit:\n%s", name, body)
	}
	return dependency.Commit
}

func assertStackNativeEvidence(t *testing.T, root, name string) {
	t.Helper()
	info, err := os.Stat(filepath.Join(root, ".stack-work"))
	if err != nil || !info.IsDir() {
		t.Fatalf("Stack project-local materialization is missing: %v", err)
	}
	commit := stackNativeLockedCommit(t, root, name)
	nativeLock := stackNativeRead(t, filepath.Join(root, "stack.yaml.lock"))
	if !strings.Contains(nativeLock, commit) {
		t.Fatalf("stack.yaml.lock has no exact %s commit %s:\n%s", name, commit, nativeLock)
	}
}

func stackNativeGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	return stackNativeRun(t, root, "git", args...)
}

func stackNativeRun(t *testing.T, root, command string, args ...string) string {
	t.Helper()
	cmd := exec.Command(command, args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_ALLOW_PROTOCOL=file")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", command, strings.Join(args, " "), err, out)
	}
	return string(out)
}

func stackNativeWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stackNativeRead(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func stackNativeFileURL(path string) string { return "file://" + filepath.ToSlash(path) }

func stackNativeQuote(value string) string { return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"` }
