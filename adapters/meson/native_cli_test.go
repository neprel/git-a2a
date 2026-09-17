package meson_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// This test is gated because it executes a real public CLI, Git submodules, and Meson.
// The pinned native runner sets GITA2A_NATIVE_BUILD_IT=meson.
func TestPublicCLIMesonLifecycle(t *testing.T) {
	if os.Getenv("GITA2A_NATIVE_BUILD_IT") != "meson" {
		t.Skip("set GITA2A_NATIVE_BUILD_IT=meson in the pinned Meson environment")
	}
	for _, tool := range []string{"git", "meson", "ninja", "cc"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("required native tool %s: %v", tool, err)
		}
	}
	_, here, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
	bin := os.Getenv("GITA2A_CLI_BIN")
	if bin == "" {
		bin = filepath.Join(t.TempDir(), "git-a2a")
		run(t, repo, "go", "build", "-o", bin, "./cmd/git-a2a")
	} else if info, err := os.Stat(bin); err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		t.Fatalf("GITA2A_CLI_BIN must name an executable file: %s: %v", bin, err)
	}

	upstream := t.TempDir()
	gitInit(t, upstream)
	write(t, upstream, "a2amodule.yml", "schema: 2\ncomponent:\n  id: native-meson\n  exports:\n    - adapter: meson\n      name: fixture_dep\n      path: lib\nagent:\n  card: https://agents.example/native-meson.json\n")
	writeMesonUpstream(t, upstream, 41)
	gitCommit(t, upstream, "initial")

	consumer := t.TempDir()
	gitInit(t, consumer)
	rootBody := "project('consumer', 'c')\nmessage('unrelated consumer declaration')\nexecutable('app', 'main.c', dependencies: fixture_dep)\n"
	write(t, consumer, "meson.build", rootBody)
	write(t, consumer, "main.c", "#include <stdio.h>\n#include <fixture.h>\nint main(void) { printf(\"%d\", fixture_value()); return 0; }\n")
	write(t, consumer, "unrelated.txt", "preserve me exactly\n")
	run(t, consumer, bin, "init", "--id", "consumer")
	run(t, consumer, bin, "add", fileURL(upstream), "--name", "fixture", "--ref", "main")
	assertBindings(t, consumer, "adapter: submodule", "adapter: meson")
	assertMesonValue(t, consumer, "41")
	gitCommit(t, consumer, "installed")

	fresh := filepath.Join(t.TempDir(), "fresh")
	run(t, filepath.Dir(fresh), "git", "clone", consumer, fresh)
	run(t, fresh, bin, "pull", "fixture")
	assertMesonValue(t, fresh, "41")

	if err := os.RemoveAll(filepath.Join(consumer, "deps", "fixture")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(consumer, ".git-a2a", "build", "meson")); err != nil {
		t.Fatal(err)
	}
	run(t, consumer, bin, "pull", "fixture")
	assertMesonValue(t, consumer, "41")

	writeMesonUpstream(t, upstream, 42)
	gitCommit(t, upstream, "same-version update")
	run(t, consumer, bin, "pull", "fixture")
	assertMesonValue(t, consumer, "42")
	gitCommit(t, consumer, "updated")
	run(t, consumer, bin, "pull", "fixture")
	if got := strings.TrimSpace(run(t, consumer, "git", "status", "--porcelain")); got != "" {
		t.Fatalf("idempotent pull left diff: %s", got)
	}

	run(t, consumer, bin, "remove", "fixture")
	if got := mustRead(t, filepath.Join(consumer, "meson.build")); got != rootBody {
		t.Fatalf("root not byte-restored:\n%s", got)
	}
	if got := mustRead(t, filepath.Join(consumer, "unrelated.txt")); got != "preserve me exactly\n" {
		t.Fatalf("unrelated content changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(consumer, "deps", "fixture")); !os.IsNotExist(err) {
		t.Fatalf("checkout remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(consumer, ".git-a2a", "build", "meson")); !os.IsNotExist(err) {
		t.Fatalf("owned Meson setup tree remains: %v", err)
	}
}

func writeMesonUpstream(t *testing.T, root string, value int) {
	t.Helper()
	write(t, root, "meson.build", "project('fixture', 'c', version: '1.0.0')\nsubdir('lib')\n")
	write(t, root, "lib/meson.build", "fixture_dep = declare_dependency(include_directories: include_directories('.'))\n")
	write(t, root, "lib/fixture.h", "#pragma once\nstatic inline int fixture_value(void) { return "+strconv.Itoa(value)+"; }\n")
}

func assertMesonValue(t *testing.T, root, want string) {
	t.Helper()
	build := filepath.Join(root, ".git-a2a", "build", "meson")
	run(t, root, "meson", "compile", "-C", build)
	if got := strings.TrimSpace(run(t, root, filepath.Join(build, "app"))); got != want {
		t.Fatalf("app value=%q want=%q", got, want)
	}
}

func assertBindings(t *testing.T, root string, wants ...string) {
	t.Helper()
	body := mustRead(t, filepath.Join(root, "a2amodule.yml"))
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("manifest lacks %q:\n%s", want, body)
		}
	}
}

func gitInit(t *testing.T, root string) {
	t.Helper()
	run(t, root, "git", "init", "-b", "main")
	run(t, root, "git", "config", "user.email", "native@example.test")
	run(t, root, "git", "config", "user.name", "Native")
}

func gitCommit(t *testing.T, root, message string) {
	t.Helper()
	run(t, root, "git", "add", "-A")
	run(t, root, "git", "commit", "-m", message)
}

func write(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func fileURL(path string) string { return "file://" + filepath.ToSlash(path) }

func run(t *testing.T, dir, command string, args ...string) string {
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
