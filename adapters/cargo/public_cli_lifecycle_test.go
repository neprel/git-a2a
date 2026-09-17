package cargo_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPublicCLICargoLifecycle exercises Cargo through the public five-command
// surface and then compiles and runs the consumer. The explicit version gate
// makes native evidence reproducible instead of silently accepting whichever
// Cargo happens to be on PATH. For example:
//
//	GITA2A_IT_CARGO_CLI=1 \
//	GITA2A_IT_CARGO_VERSION='cargo 1.97.1 (c980f4866 2026-06-30)' \
//	go test ./adapters/cargo -run TestPublicCLICargoLifecycle -v
func TestPublicCLICargoLifecycle(t *testing.T) {
	if os.Getenv("GITA2A_IT_CARGO_CLI") != "1" {
		t.Skip("set GITA2A_IT_CARGO_CLI=1 in the pinned Cargo environment")
	}
	wantVersion := os.Getenv("GITA2A_IT_CARGO_VERSION")
	if wantVersion == "" {
		t.Fatal("GITA2A_IT_CARGO_VERSION must contain the complete pinned `cargo --version` output")
	}
	for _, tool := range []string{"git", "cargo"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("required native tool %s: %v", tool, err)
		}
	}
	if got := strings.TrimSpace(runCargoNative(t, "", "cargo", "--version")); got != wantVersion {
		t.Fatalf("cargo version = %q, pinned environment requires %q", got, wantVersion)
	} else {
		t.Logf("native toolchain: %s (%s/%s)", got, runtime.GOOS, runtime.GOARCH)
	}

	bin := buildCargoCLI(t)
	upstream := t.TempDir()
	gitCargoInit(t, upstream)
	writeCargoFile(t, filepath.Join(upstream, "a2amodule.yml"), "schema: 2\ncomponent:\n  id: native-cargo\n  exports:\n    - adapter: cargo\n      name: native-lib\nagent:\n  card: https://agents.example/native-cargo.json\n")
	writeCargoUpstream(t, upstream, "one")
	gitCargoCommit(t, upstream, "initial")

	consumer := t.TempDir()
	writeCargoFile(t, filepath.Join(consumer, "Cargo.toml"), `[package]
name = "consumer"
version = "0.1.0"
edition = "2021"

[dependencies]
unrelated = { path = "unrelated" } # user-owned dependency
`)
	writeCargoFile(t, filepath.Join(consumer, "src", "main.rs"), `fn main() { println!("{}", unrelated::value()); }
`)
	writeCargoFile(t, filepath.Join(consumer, "examples", "native.rs"), `fn main() { println!("{}:{}", native_lib::value(), unrelated::value()); }
`)
	writeCargoFile(t, filepath.Join(consumer, "unrelated", "Cargo.toml"), `[package]
name = "unrelated"
version = "7.0.0"
edition = "2021"
`)
	writeCargoFile(t, filepath.Join(consumer, "unrelated", "src", "lib.rs"), "pub fn value() -> u8 { 7 }\n")

	// An isolated Cargo home proves fetch/materialization and keeps this test
	// independent of (and harmless to) the user's shared Cargo cache.
	cargoHome := filepath.Join(t.TempDir(), "cargo-home")
	setCargoHome(t, cargoHome)
	runCargoNative(t, consumer, bin, "init", "--id", "consumer")
	runCargoNative(t, consumer, bin, "add", cargoFileURL(upstream), "--name", "fixture", "--ref", "main")
	assertCargoValues(t, consumer, "one:7")
	v1 := strings.TrimSpace(runCargoNative(t, upstream, "git", "rev-parse", "HEAD"))
	assertCargoLockRevision(t, consumer, v1)

	gitCargoInit(t, consumer)
	runCargoNative(t, consumer, "git", "add", ".gitignore", "a2amodule.yml", "a2amodule.lock", "Cargo.toml", "Cargo.lock", "src", "examples", "unrelated")
	runCargoNative(t, consumer, "git", "commit", "-m", "installed")

	// A fresh clone with an empty Cargo cache must be made usable by pull.
	clearCargoHome(t, cargoHome)
	fresh := filepath.Join(t.TempDir(), "fresh")
	runCargoNative(t, filepath.Dir(fresh), "git", "clone", consumer, fresh)
	runCargoNative(t, fresh, bin, "pull", "fixture")
	assertCargoValues(t, fresh, "one:7")

	// Losing all fetched Git materialization at an unchanged commit must also
	// be repaired, even though Cargo.toml and Cargo.lock already agree.
	clearCargoHome(t, cargoHome)
	runCargoNative(t, consumer, bin, "pull", "fixture")
	assertCargoValues(t, consumer, "one:7")

	// The package version remains 1.0.0; targeted pull must follow the branch
	// to its new Git commit and make that exact source executable.
	writeCargoUpstream(t, upstream, "two")
	gitCargoCommit(t, upstream, "same version, second revision")
	v2 := strings.TrimSpace(runCargoNative(t, upstream, "git", "rev-parse", "HEAD"))
	runCargoNative(t, consumer, bin, "pull", "fixture")
	assertCargoValues(t, consumer, "two:7")
	assertCargoLockRevision(t, consumer, v2)
	if v1 == v2 {
		t.Fatal("upstream commit did not advance")
	}

	runCargoNative(t, consumer, bin, "remove", "fixture")
	manifest := readCargoFile(t, filepath.Join(consumer, "Cargo.toml"))
	if strings.Contains(manifest, "native-lib") {
		t.Fatalf("removed root dependency remains:\n%s", manifest)
	}
	if !strings.Contains(manifest, `unrelated = { path = "unrelated" } # user-owned dependency`) {
		t.Fatalf("unrelated declaration was not preserved:\n%s", manifest)
	}
	lock := readCargoFile(t, filepath.Join(consumer, "Cargo.lock"))
	if strings.Contains(lock, `name = "native-lib"`) {
		t.Fatalf("removed package remains in Cargo.lock:\n%s", lock)
	}
	if !strings.Contains(lock, `name = "unrelated"`) {
		t.Fatalf("unrelated package was removed from Cargo.lock:\n%s", lock)
	}
	if got := strings.TrimSpace(runCargoNative(t, consumer, "cargo", "run", "--quiet")); got != "7" {
		t.Fatalf("unrelated dependency value = %q, want 7", got)
	}
}

func writeCargoUpstream(t *testing.T, root, value string) {
	t.Helper()
	writeCargoFile(t, filepath.Join(root, "Cargo.toml"), `[package]
name = "native-lib"
version = "1.0.0"
edition = "2021"
`)
	writeCargoFile(t, filepath.Join(root, "src", "lib.rs"), `pub fn value() -> &'static str { "`+value+`" }
`)
}

func assertCargoValues(t *testing.T, root, want string) {
	t.Helper()
	got := strings.TrimSpace(runCargoNative(t, root, "cargo", "run", "--quiet", "--example", "native"))
	if got != want {
		t.Fatalf("compiled dependency value = %q, want %q", got, want)
	}
}

func assertCargoLockRevision(t *testing.T, root, commit string) {
	t.Helper()
	lock := readCargoFile(t, filepath.Join(root, "Cargo.lock"))
	if !strings.Contains(lock, `name = "native-lib"`) || !strings.Contains(lock, "#"+commit) {
		t.Fatalf("Cargo.lock does not bind native-lib to %s:\n%s", commit, lock)
	}
}

func buildCargoCLI(t *testing.T) string {
	t.Helper()
	if binary := os.Getenv("GITA2A_CLI_BIN"); binary != "" {
		return binary
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	bin := filepath.Join(t.TempDir(), "git-a2a")
	runCargoNative(t, repo, "go", "build", "-o", bin, "./cmd/git-a2a")
	return bin
}

func gitCargoInit(t *testing.T, root string) {
	t.Helper()
	runCargoNative(t, root, "git", "init", "-b", "main")
	runCargoNative(t, root, "git", "config", "user.email", "native@example.test")
	runCargoNative(t, root, "git", "config", "user.name", "Native Test")
}

func gitCargoCommit(t *testing.T, root, message string) {
	t.Helper()
	runCargoNative(t, root, "git", "add", "-A")
	runCargoNative(t, root, "git", "commit", "-m", message)
}

func setCargoHome(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CARGO_HOME", root)
}

func clearCargoHome(t *testing.T, root string) {
	t.Helper()
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeCargoFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readCargoFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func cargoFileURL(path string) string {
	slashed := filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		return "file:///" + strings.TrimPrefix(slashed, "/")
	}
	return "file://" + slashed
}

func runCargoNative(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_ALLOW_PROTOCOL=file")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}
