package hackage_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPublicCLICabalLifecycle is opt-in because it compiles real Haskell
// packages. The central native-lifecycle runner supplies the pinned toolchain and turns the
// missing-tool case into a hard failure rather than a successful skip.
func TestPublicCLICabalLifecycle(t *testing.T) {
	if os.Getenv("GITA2A_NATIVE_HACKAGE_IT") != "cabal" {
		t.Skip("run the cabal case through tools/native-lifecycle/run.py")
	}
	for _, tool := range []string{"git", "cabal", "ghc"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("required native tool %s: %v", tool, err)
		}
	}
	cabalHome := os.Getenv("GITA2A_CABAL_HOME")
	if cabalHome == "" {
		cabalHome = filepath.Join(t.TempDir(), "cabal-home")
	}
	t.Setenv("CABAL_DIR", cabalHome)

	bin := cabalNativeCLI(t)
	base := t.TempDir()
	fixture := filepath.Join(base, "fixture")
	stable := filepath.Join(base, "stable")
	cabalNativeUpstream(t, fixture, "fixture-lib", "FixtureLib", "fixtureValue", "one")
	cabalNativeUpstream(t, stable, "stable-lib", "StableLib", "stableValue", "stable-one")
	fixtureFirst := strings.TrimSpace(cabalNativeRun(t, fixture, "git", "rev-parse", "HEAD"))
	stableFirst := strings.TrimSpace(cabalNativeRun(t, stable, "git", "rev-parse", "HEAD"))

	consumer := filepath.Join(base, "consumer")
	cabalNativeGitInit(t, consumer)
	cabalNativeWrite(t, consumer, ".gitignore", "dist-newstyle/\n")
	cabalNativeWrite(t, consumer, "cabal.project", "packages:\n  consumer\n  unrelated\nindex-state: 2024-01-01T00:00:00Z\n\n-- unrelated-user-content\n")
	cabalNativeWrite(t, consumer, "consumer/consumer.cabal", cabalNativeConsumerCabal(false))
	cabalNativeWrite(t, consumer, "consumer/app/Main.hs", cabalNativeMain(false))
	cabalNativeWrite(t, consumer, "unrelated/unrelated-lib.cabal", `cabal-version: 3.0
name: unrelated-lib
version: 1.0.0
build-type: Simple
library
  exposed-modules: UnrelatedLib
  hs-source-dirs: src
  build-depends: base >=4.15 && <5
  default-language: Haskell2010
`)
	cabalNativeWrite(t, consumer, "unrelated/src/UnrelatedLib.hs", "module UnrelatedLib (unrelatedValue) where\nunrelatedValue :: String\nunrelatedValue = \"unrelated\"\n")

	cabalNativeRun(t, consumer, bin, "init", "--id", "consumer")
	cabalNativeRun(t, consumer, bin, "add", cabalNativeFileURL(stable), "--name", "stable", "--ref", "main")
	cabalNativeAssertValue(t, consumer, "stable-one:unrelated")
	cabalNativeWrite(t, consumer, "consumer/consumer.cabal", cabalNativeConsumerCabal(true))
	cabalNativeWrite(t, consumer, "consumer/app/Main.hs", cabalNativeMain(true))
	cabalNativeRun(t, consumer, bin, "add", cabalNativeFileURL(fixture), "--name", "fixture", "--ref", "main")
	cabalNativeAssertValue(t, consumer, "one:stable-one:unrelated")
	cabalNativeAssertSourceRevision(t, consumer, cabalNativeFileURL(fixture), fixtureFirst)
	cabalNativeAssertSourceRevision(t, consumer, cabalNativeFileURL(stable), stableFirst)
	cabalNativeAssertDependency(t, consumer, "fixture", true)
	cabalNativeAssertDependency(t, consumer, "stable", true)
	cabalNativeGitCommit(t, consumer, "installed")

	// A clone deliberately has neither Cabal's project build tree nor any
	// download cache. Public pull must make the saved dependency usable again.
	fresh := filepath.Join(base, "fresh")
	cabalNativeRun(t, base, "git", "clone", consumer, fresh)
	cabalNativeRun(t, fresh, bin, "pull", "fixture")
	cabalNativeAssertValue(t, fresh, "one:stable-one:unrelated")
	cabalNativeAssertSourceRevision(t, fresh, cabalNativeFileURL(fixture), fixtureFirst)

	// Losing project-owned materialization at an unchanged commit is repair,
	// not a no-op.
	if err := os.RemoveAll(filepath.Join(consumer, "dist-newstyle")); err != nil {
		t.Fatal(err)
	}
	cabalNativeRun(t, consumer, bin, "pull", "fixture")
	cabalNativeAssertValue(t, consumer, "one:stable-one:unrelated")

	// Both remotes advance, but a targeted pull may change only fixture.
	cabalNativeSetValue(t, stable, "StableLib", "stableValue", "stable-two")
	cabalNativeGitCommit(t, stable, "stable two")
	cabalNativeSetValue(t, fixture, "FixtureLib", "fixtureValue", "two")
	cabalNativeGitCommit(t, fixture, "fixture two")
	fixtureSecond := strings.TrimSpace(cabalNativeRun(t, fixture, "git", "rev-parse", "HEAD"))
	cabalNativeRun(t, consumer, bin, "pull", "fixture")
	cabalNativeAssertValue(t, consumer, "two:stable-one:unrelated")
	cabalNativeAssertSourceRevision(t, consumer, cabalNativeFileURL(fixture), fixtureSecond)
	cabalNativeAssertSourceRevision(t, consumer, cabalNativeFileURL(stable), stableFirst)
	cabalNativeGitCommit(t, consumer, "fixture updated")
	cabalNativeRun(t, consumer, bin, "pull", "fixture")
	if got := strings.TrimSpace(cabalNativeRun(t, consumer, "git", "status", "--porcelain")); got != "" {
		t.Fatalf("idempotent targeted pull left a tracked diff: %s", got)
	}

	// Usage declarations are user-owned. Remove them first, then prove that
	// git-a2a removes only its fixture root while stable and unrelated compile.
	cabalNativeWrite(t, consumer, "consumer/consumer.cabal", cabalNativeConsumerCabal(false))
	cabalNativeWrite(t, consumer, "consumer/app/Main.hs", cabalNativeMain(false))
	cabalNativeRun(t, consumer, bin, "remove", "fixture")
	cabalNativeAssertValue(t, consumer, "stable-one:unrelated")
	cabalNativeAssertDependency(t, consumer, "fixture", false)
	cabalNativeAssertDependency(t, consumer, "stable", true)
	project := cabalNativeRead(t, filepath.Join(consumer, "cabal.project"))
	if strings.Contains(project, "git-a2a:begin fixture") || strings.Contains(project, cabalNativeFileURL(fixture)) {
		t.Fatalf("removed Cabal root survived:\n%s", project)
	}
	if !strings.Contains(project, "-- unrelated-user-content") {
		t.Fatalf("unrelated Cabal content was lost:\n%s", project)
	}
}

func cabalNativeCLI(t *testing.T) string {
	t.Helper()
	if bin := os.Getenv("GITA2A_CLI_BIN"); bin != "" {
		info, err := os.Stat(bin)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			t.Fatalf("GITA2A_CLI_BIN must be executable: %s: %v", bin, err)
		}
		return bin
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	bin := filepath.Join(t.TempDir(), "git-a2a")
	cabalNativeRun(t, repo, "go", "build", "-o", bin, "./cmd/git-a2a")
	return bin
}

func cabalNativeUpstream(t *testing.T, root, packageName, module, functionName, value string) {
	t.Helper()
	cabalNativeGitInit(t, root)
	cabalNativeWrite(t, root, "a2amodule.yml", "schema: 2\ncomponent:\n  id: "+packageName+"\n  exports:\n    - adapter: hackage\n      name: "+packageName+"\nagent:\n  card: https://agents.example/"+packageName+".json\n")
	cabalNativeWrite(t, root, packageName+".cabal", "cabal-version: 3.0\nname: "+packageName+"\nversion: 1.0.0\nbuild-type: Simple\nlibrary\n  exposed-modules: "+module+"\n  hs-source-dirs: src\n  build-depends: base >=4.15 && <5\n  default-language: Haskell2010\n")
	cabalNativeSetValue(t, root, module, functionName, value)
	cabalNativeGitCommit(t, root, "initial")
}

func cabalNativeSetValue(t *testing.T, root, module, functionName, value string) {
	t.Helper()
	cabalNativeWrite(t, root, "src/"+module+".hs", "module "+module+" ("+functionName+") where\n"+functionName+" :: String\n"+functionName+" = \""+value+"\"\n")
}

func cabalNativeConsumerCabal(withFixture bool) string {
	dependencies := "base >=4.15 && <5, stable-lib, unrelated-lib"
	if withFixture {
		dependencies += ", fixture-lib"
	}
	return "cabal-version: 3.0\nname: consumer\nversion: 1.0.0\nbuild-type: Simple\nexecutable consumer\n  main-is: Main.hs\n  hs-source-dirs: app\n  build-depends: " + dependencies + "\n  default-language: Haskell2010\n"
}

func cabalNativeMain(withFixture bool) string {
	if withFixture {
		return "module Main where\nimport FixtureLib (fixtureValue)\nimport StableLib (stableValue)\nimport UnrelatedLib (unrelatedValue)\nmain :: IO ()\nmain = putStr (fixtureValue ++ \":\" ++ stableValue ++ \":\" ++ unrelatedValue)\n"
	}
	return "module Main where\nimport StableLib (stableValue)\nimport UnrelatedLib (unrelatedValue)\nmain :: IO ()\nmain = putStr (stableValue ++ \":\" ++ unrelatedValue)\n"
}

func cabalNativeAssertValue(t *testing.T, root, want string) {
	t.Helper()
	got := strings.TrimSpace(cabalNativeRun(t, root, "cabal", "run", "consumer", "--offline"))
	lines := strings.Split(got, "\n")
	if got = strings.TrimSpace(lines[len(lines)-1]); got != want {
		t.Fatalf("Cabal consumer output=%q, want %q\nfull output:\n%s", got, want, strings.Join(lines, "\n"))
	}
}

func cabalNativeAssertDependency(t *testing.T, root, name string, present bool) {
	t.Helper()
	body := cabalNativeRead(t, filepath.Join(root, "a2amodule.yml"))
	found := strings.Contains(body, "- name: "+name+"\n") || strings.Contains(body, "  name: "+name+"\n")
	if found != present {
		t.Fatalf("dependency %s presence=%v, want %v:\n%s", name, found, present, body)
	}
}

func cabalNativeAssertSourceRevision(t *testing.T, root, wantURL, wantCommit string) {
	t.Helper()
	sourceRoot := filepath.Join(root, "dist-newstyle", "src")
	entries, err := os.ReadDir(sourceRoot)
	if err != nil {
		t.Fatalf("read Cabal source materialization: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		checkout := filepath.Join(sourceRoot, entry.Name())
		if _, err := os.Stat(filepath.Join(checkout, ".git")); err != nil {
			continue
		}
		gotURL := strings.TrimSpace(cabalNativeRun(t, checkout, "git", "config", "--get", "remote.origin.url"))
		if gotURL != wantURL {
			continue
		}
		gotCommit := strings.TrimSpace(cabalNativeRun(t, checkout, "git", "rev-parse", "HEAD"))
		if gotCommit != wantCommit {
			t.Fatalf("Cabal checkout %s at %s, want %s", checkout, gotCommit, wantCommit)
		}
		planPath := filepath.Join(root, "dist-newstyle", "cache", "plan.json")
		plan, readErr := os.ReadFile(planPath)
		if readErr != nil {
			t.Fatalf("read Cabal plan %s: %v", planPath, readErr)
		}
		t.Logf("Cabal plan %s contains exact commit %s: %v; checkout %s HEAD is exact", planPath, wantCommit, strings.Contains(string(plan), wantCommit), checkout)
		return
	}
	t.Fatalf("Cabal materialization for %s at %s not found below %s", wantURL, wantCommit, sourceRoot)
}

func cabalNativeGitInit(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cabalNativeRun(t, root, "git", "init", "-b", "main")
	cabalNativeRun(t, root, "git", "config", "user.email", "native@example.test")
	cabalNativeRun(t, root, "git", "config", "user.name", "Native Cabal")
}

func cabalNativeGitCommit(t *testing.T, root, message string) {
	t.Helper()
	cabalNativeRun(t, root, "git", "add", "-A")
	cabalNativeRun(t, root, "git", "commit", "-m", message)
}

func cabalNativeWrite(t *testing.T, root, relative, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func cabalNativeRead(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func cabalNativeFileURL(path string) string { return "file://" + filepath.ToSlash(path) }

func cabalNativeRun(t *testing.T, dir, command string, args ...string) string {
	t.Helper()
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_ALLOW_PROTOCOL=file")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", command, strings.Join(args, " "), err, out)
	}
	return string(out)
}
