package zig

import (
	"fmt"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	lockfile "github.com/neprel/git-a2a/internal/lock"
)

// TestPublicCLIZigLifecycle is opt-in because it requires real Zig 0.14.1.
// The pinned central native-lifecycle runner enables it and makes absence a hard failure.
func TestPublicCLIZigLifecycle(t *testing.T) {
	if os.Getenv("GITA2A_IT_ZIG_CLI") != "1" {
		t.Skip("run the zig case through tools/native-lifecycle/run.py")
	}
	if got := strings.TrimSpace(zigRun(t, "", nil, "zig", "version")); got != "0.14.1" {
		t.Fatalf("zig version = %q, want 0.14.1", got)
	}
	bin := zigCLIBinary(t)
	base := t.TempDir()
	primary := newZigRemote(t, base, "native_primary", "primary-one")
	other := newZigRemote(t, base, "native_other", "other-one")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(&cgi.Handler{
		Path: git,
		Args: []string{"http-backend"},
		Env:  []string{"GIT_PROJECT_ROOT=" + base, "GIT_HTTP_EXPORT_ALL=1"},
	})
	t.Cleanup(server.Close)
	primary.url = server.URL + "/" + filepath.Base(primary.bare)
	other.url = server.URL + "/" + filepath.Base(other.bare)

	consumer := filepath.Join(t.TempDir(), "consumer")
	writeZigConsumer(t, consumer, false, false)
	zigCLI(t, consumer, bin, "init", "--id", "zig-consumer")
	zigCLI(t, consumer, bin, "add", primary.url, "--name", "primary", "--ref", "main")
	zigCLI(t, consumer, bin, "add", other.url, "--name", "other", "--ref", "main")
	writeZigConsumer(t, consumer, true, true)
	assertZigUse(t, consumer, "primary-one:other-one:local")
	initialLock := zigRead(t, filepath.Join(consumer, "a2amodule.lock"))

	// A fresh clone has no cache, but targeted Pull must make it usable.
	zigRun(t, consumer, nil, "git", "init", "-b", "main")
	zigRun(t, consumer, nil, "git", "config", "user.name", "git-a2a Zig native")
	zigRun(t, consumer, nil, "git", "config", "user.email", "zig-native@example.invalid")
	zigRun(t, consumer, nil, "git", "add", ".gitignore", "a2amodule.yml", "a2amodule.lock", "build.zig", "build.zig.zon", "src", "unrelated")
	zigRun(t, consumer, nil, "git", "commit", "-m", "installed")
	fresh := filepath.Join(t.TempDir(), "fresh")
	zigRun(t, "", nil, "git", "clone", consumer, fresh)
	zigCLI(t, fresh, bin, "pull", "primary")
	assertZigUse(t, fresh, "primary-one:other-one:local")

	// Repair all project-scoped cache state at the same locked commit.
	if err := os.RemoveAll(filepath.Join(consumer, ".zig-global-cache")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(consumer, ".zig-cache")); err != nil {
		t.Fatal(err)
	}
	zonMissing := zigRead(t, filepath.Join(consumer, "build.zig.zon"))
	lockMissing := zigRead(t, filepath.Join(consumer, "a2amodule.lock"))
	listed := zigCLI(t, consumer, bin, "list", "primary")
	if !strings.Contains(listed, "problem:") {
		t.Fatalf("list did not report missing Zig materialization:\n%s", listed)
	}
	if _, err := os.Stat(filepath.Join(consumer, ".zig-global-cache")); !os.IsNotExist(err) {
		t.Fatalf("read-only list recreated Zig cache: %v", err)
	}
	if got := zigRead(t, filepath.Join(consumer, "build.zig.zon")); string(got) != string(zonMissing) {
		t.Fatal("read-only list changed build.zig.zon")
	}
	if got := zigRead(t, filepath.Join(consumer, "a2amodule.lock")); string(got) != string(lockMissing) {
		t.Fatal("read-only list changed a2amodule.lock")
	}
	zigCLI(t, consumer, bin, "pull", "primary")
	assertZigUse(t, consumer, "primary-one:other-one:local")

	// Both remotes advance without a version bump. Targeted Pull advances one.
	primary.advance(t, "primary-two")
	other.advance(t, "other-two")
	otherBefore := zigLockedCommit(t, consumer, "other")
	zigCLI(t, consumer, bin, "pull", "primary")
	assertZigUse(t, consumer, "primary-two:other-one:local")
	if got := zigLockedCommit(t, consumer, "other"); got != otherBefore {
		t.Fatalf("targeted pull advanced unrelated dependency: got %s want %s", got, otherBefore)
	}
	if string(zigRead(t, filepath.Join(consumer, "a2amodule.lock"))) == string(initialLock) {
		t.Fatal("primary lock did not advance")
	}

	// Repeating the same Pull is byte-idempotent for both declarations and lock.
	zonBefore := zigRead(t, filepath.Join(consumer, "build.zig.zon"))
	lockBefore := zigRead(t, filepath.Join(consumer, "a2amodule.lock"))
	zigCLI(t, consumer, bin, "pull", "primary")
	if got := zigRead(t, filepath.Join(consumer, "build.zig.zon")); string(got) != string(zonBefore) {
		t.Fatal("idempotent pull changed build.zig.zon")
	}
	if got := zigRead(t, filepath.Join(consumer, "a2amodule.lock")); string(got) != string(lockBefore) {
		t.Fatal("idempotent pull changed a2amodule.lock")
	}

	// Usage is consumer-owned; remove it before the root dependency declaration.
	writeZigConsumer(t, consumer, false, true)
	zigCLI(t, consumer, bin, "remove", "primary")
	assertZigUse(t, consumer, "other-one:local")
	zon := string(zigRead(t, filepath.Join(consumer, "build.zig.zon")))
	if strings.Contains(zon, "native_primary") || !strings.Contains(zon, "native_other") || !strings.Contains(zon, `.path = "unrelated"`) {
		t.Fatalf("unexpected dependencies after remove:\n%s", zon)
	}
	if strings.Contains(string(zigRead(t, filepath.Join(consumer, "a2amodule.yml"))), "name: primary") {
		t.Fatal("primary git-a2a declaration survived remove")
	}
}

type zigRemote struct{ work, bare, url, packageName string }

func newZigRemote(t *testing.T, base, name, value string) *zigRemote {
	t.Helper()
	work, bare := filepath.Join(base, name+"-work"), filepath.Join(base, name+".git")
	writeZigPackage(t, work, name, value)
	zigRun(t, work, nil, "git", "init", "-b", "main")
	zigRun(t, work, nil, "git", "config", "user.name", "git-a2a Zig native")
	zigRun(t, work, nil, "git", "config", "user.email", "zig-native@example.invalid")
	writeZigManifest(t, work, name, zigPackageHash(t, work))
	zigRun(t, work, nil, "git", "add", ".")
	zigRun(t, work, nil, "git", "commit", "-m", value)
	zigRun(t, "", nil, "git", "clone", "--bare", work, bare)
	zigRun(t, bare, nil, "git", "update-server-info")
	return &zigRemote{work: work, bare: bare, packageName: name}
}

func (r *zigRemote) advance(t *testing.T, value string) {
	t.Helper()
	zigWrite(t, filepath.Join(r.work, "src", "root.zig"), fmt.Sprintf("pub fn value() []const u8 { return %q; }\n", value))
	writeZigManifest(t, r.work, r.packageName, zigPackageHash(t, r.work))
	zigRun(t, r.work, nil, "git", "add", ".")
	zigRun(t, r.work, nil, "git", "commit", "-m", value)
	zigRun(t, r.work, nil, "git", "push", r.bare, "main")
	zigRun(t, r.bare, nil, "git", "update-server-info")
}

func writeZigPackage(t *testing.T, root, name, value string) {
	t.Helper()
	zon := fmt.Sprintf(`.{
    .name = .%s,
    .version = "1.0.0",
    .fingerprint = %s,
    .minimum_zig_version = "0.14.1",
    .paths = .{ "build.zig", "build.zig.zon", "src" },
}
`, name, zigFingerprint(t, name))
	build := fmt.Sprintf("const std = @import(\"std\");\npub fn build(b: *std.Build) void { _ = b.addModule(%q, .{ .root_source_file = b.path(\"src/root.zig\") }); }\n", name)
	zigWrite(t, filepath.Join(root, "build.zig.zon"), zon)
	zigWrite(t, filepath.Join(root, "build.zig"), build)
	zigWrite(t, filepath.Join(root, "src", "root.zig"), fmt.Sprintf("pub fn value() []const u8 { return %q; }\n", value))
}

func writeZigManifest(t *testing.T, root, name, hash string) {
	t.Helper()
	body := fmt.Sprintf("schema: 2\ncomponent:\n  id: %s\n  exports:\n    - adapter: zig\n      name: %s\n      checksum: %s\nagent:\n  card: https://agents.example/%s.json\n", name, name, hash, name)
	zigWrite(t, filepath.Join(root, "a2amodule.yml"), body)
}

func zigFingerprint(t *testing.T, name string) string {
	t.Helper()
	root := t.TempDir()
	zigWrite(t, filepath.Join(root, "build.zig.zon"), fmt.Sprintf(`.{ .name = .%s, .version = "1.0.0", .fingerprint = 0x1111111111111111, .minimum_zig_version = "0.14.1" }
`, name))
	cmd := exec.Command("zig", "fetch", root)
	cmd.Env = zigEnvironment(root)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("zig accepted deliberately invalid fingerprint for %s", name)
	}
	match := regexp.MustCompile(`use this value: (0x[0-9a-f]+)`).FindStringSubmatch(string(output))
	if len(match) != 2 {
		t.Fatalf("discover Zig fingerprint for %s: %v\n%s", name, err, output)
	}
	return match[1]
}

func zigPackageHash(t *testing.T, root string) string {
	t.Helper()
	return strings.TrimSpace(zigRun(t, root, zigEnvironment(t.TempDir()), "zig", "fetch", root))
}

func writeZigConsumer(t *testing.T, root string, primary, other bool) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, "build.zig.zon")); os.IsNotExist(err) {
		zon := fmt.Sprintf(`.{
    .name = .zig_consumer,
    .version = "1.0.0",
    .fingerprint = %s,
    .minimum_zig_version = "0.14.1",
    .paths = .{ "build.zig", "build.zig.zon", "src", "unrelated" },
    .dependencies = .{
        .unrelated = .{ .path = "unrelated" },
    },
}
`, zigFingerprint(t, "zig_consumer"))
		zigWrite(t, filepath.Join(root, "build.zig.zon"), zon)
		unrelatedZON := fmt.Sprintf(`.{ .name = .unrelated, .version = "1.0.0", .fingerprint = %s, .minimum_zig_version = "0.14.1", .paths = .{ "build.zig", "build.zig.zon", "src" } }
`, zigFingerprint(t, "unrelated"))
		zigWrite(t, filepath.Join(root, "unrelated", "build.zig.zon"), unrelatedZON)
		zigWrite(t, filepath.Join(root, "unrelated", "build.zig"), "const std = @import(\"std\");\npub fn build(b: *std.Build) void { _ = b.addModule(\"unrelated\", .{ .root_source_file = b.path(\"src/root.zig\") }); }\n")
		zigWrite(t, filepath.Join(root, "unrelated", "src", "root.zig"), "pub fn value() []const u8 { return \"local\"; }\n")
	}

	imports := "    exe.root_module.addImport(\"unrelated\", b.dependency(\"unrelated\", .{}).module(\"unrelated\"));\n"
	mainImports := "const std = @import(\"std\");\nconst unrelated = @import(\"unrelated\");\n"
	values, format := "unrelated.value()", "{s}\n"
	if other {
		imports += "    exe.root_module.addImport(\"native_other\", b.dependency(\"native_other\", .{}).module(\"native_other\"));\n"
		mainImports += "const other = @import(\"native_other\");\n"
		values, format = "other.value(), "+values, "{s}:{s}\n"
	}
	if primary {
		imports += "    exe.root_module.addImport(\"native_primary\", b.dependency(\"native_primary\", .{}).module(\"native_primary\"));\n"
		mainImports += "const primary = @import(\"native_primary\");\n"
		values, format = "primary.value(), "+values, "{s}:{s}:{s}\n"
	}
	build := `const std = @import("std");
pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});
    const exe = b.addExecutable(.{ .name = "consumer", .root_module = b.createModule(.{
        .root_source_file = b.path("src/main.zig"), .target = target, .optimize = optimize,
    }) });
` + imports + `    const run = b.addRunArtifact(exe);
    b.step("run", "Run consumer").dependOn(&run.step);
}
`
	zigWrite(t, filepath.Join(root, "build.zig"), build)
	zigWrite(t, filepath.Join(root, "src", "main.zig"), mainImports+fmt.Sprintf("pub fn main() !void { std.debug.print(%q, .{%s}); }\n", format, values))
}

func assertZigUse(t *testing.T, root, want string) {
	t.Helper()
	got := strings.TrimSpace(zigRun(t, root, zigEnvironment(root), "zig", "build", "run"))
	if got != want {
		t.Fatalf("Zig consumer output = %q, want %q", got, want)
	}
}

func zigLockedCommit(t *testing.T, root, name string) string {
	t.Helper()
	current, err := lockfile.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	dependency, ok := current.Dependencies[name]
	if !ok {
		t.Fatalf("lock has no dependency %s", name)
	}
	return dependency.Commit
}

func zigCLIBinary(t *testing.T) string {
	t.Helper()
	if binary := os.Getenv("GITA2A_CLI_BIN"); binary != "" {
		if info, err := os.Stat(binary); err != nil || info.IsDir() {
			t.Fatalf("GITA2A_CLI_BIN %q is not an executable: %v", binary, err)
		}
		return binary
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	binary := filepath.Join(t.TempDir(), "git-a2a")
	zigRun(t, repo, nil, "go", "build", "-o", binary, "./cmd/git-a2a")
	return binary
}

func zigCLI(t *testing.T, root, binary string, args ...string) string {
	t.Helper()
	return zigRun(t, root, zigEnvironment(root), binary, args...)
}

func zigEnvironment(root string) []string {
	environment := append([]string{}, os.Environ()...)
	if root != "" {
		environment = append(environment,
			"ZIG_GLOBAL_CACHE_DIR="+filepath.Join(root, ".zig-global-cache"),
			"ZIG_LOCAL_CACHE_DIR="+filepath.Join(root, ".zig-cache"),
		)
	}
	return append(environment, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
}

func zigRun(t *testing.T, root string, environment []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = root
	if environment == nil {
		environment = os.Environ()
	}
	cmd.Env = environment
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func zigWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func zigRead(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
