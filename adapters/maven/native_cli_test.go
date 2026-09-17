package maven_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// This test is gated because it executes a real public CLI, Git submodules, and Maven.
// The pinned native runner sets GITA2A_NATIVE_BUILD_IT=maven.
func TestPublicCLIMavenLifecycle(t *testing.T) {
	if os.Getenv("GITA2A_NATIVE_BUILD_IT") != "maven" {
		t.Skip("set GITA2A_NATIVE_BUILD_IT=maven in the pinned Maven environment")
	}
	for _, tool := range []string{"git", "mvn", "java"} {
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
	} else {
		info, err := os.Stat(bin)
		if err != nil {
			t.Fatalf("GITA2A_CLI_BIN %s: %v", bin, err)
		}
		if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("GITA2A_CLI_BIN %s is not executable", bin)
		}
	}

	upstream := t.TempDir()
	gitInit(t, upstream)
	write(t, upstream, "a2amodule.yml", "schema: 2\ncomponent:\n  id: native-maven\n  exports:\n    - adapter: maven\n      name: example.fixture:fixture-lib\nagent:\n  card: https://agents.example/native-maven.json\n")
	write(t, upstream, ".gitignore", "target/\n")
	writeMavenUpstream(t, upstream, 41)
	gitCommit(t, upstream, "initial")

	consumer := t.TempDir()
	gitInit(t, consumer)
	rootBody := `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>example.consumer</groupId>
  <artifactId>consumer</artifactId>
  <version>1.0.0</version>
  <packaging>pom</packaging>
  <modules>
    <module>app</module>
  </modules>
  <properties>
    <maven.compiler.release>17</maven.compiler.release>
    <project.build.sourceEncoding>UTF-8</project.build.sourceEncoding>
    <unrelated.user.property>preserve-me</unrelated.user.property>
  </properties>
  <!-- unrelated-user-content -->
</project>
`
	write(t, consumer, "pom.xml", rootBody)
	write(t, consumer, "app/pom.xml", `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <parent>
    <groupId>example.consumer</groupId>
    <artifactId>consumer</artifactId>
    <version>1.0.0</version>
  </parent>
  <artifactId>app</artifactId>
</project>
`)
	write(t, consumer, "app/src/main/java/example/consumer/App.java", "package example.consumer;\nimport example.fixture.Fixture;\npublic final class App { public static void main(String[] args) { System.out.print(Fixture.value()); } }\n")
	write(t, consumer, ".gitignore", "target/\n")
	write(t, consumer, "unrelated.txt", "preserve me exactly\n")
	run(t, consumer, bin, "init", "--id", "consumer")
	run(t, consumer, bin, "add", fileURL(upstream), "--name", "fixture", "--ref", "main")
	assertBindings(t, consumer, "adapter: submodule", "adapter: maven")
	assertMavenValue(t, consumer, "41")
	gitCommit(t, consumer, "installed")

	fresh := filepath.Join(t.TempDir(), "fresh")
	run(t, filepath.Dir(fresh), "git", "clone", consumer, fresh)
	run(t, fresh, bin, "pull", "fixture")
	assertMavenValue(t, fresh, "41")

	if err := os.RemoveAll(filepath.Join(consumer, "deps", "fixture")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(consumer, "app", "target")); err != nil {
		t.Fatal(err)
	}
	run(t, consumer, bin, "pull", "fixture")
	assertMavenValue(t, consumer, "41")

	writeMavenUpstream(t, upstream, 42)
	gitCommit(t, upstream, "same-version update")
	run(t, consumer, bin, "pull", "fixture")
	assertMavenValue(t, consumer, "42")
	gitCommit(t, consumer, "updated")
	run(t, consumer, bin, "pull", "fixture")
	if got := strings.TrimSpace(run(t, consumer, "git", "status", "--porcelain")); got != "" {
		t.Fatalf("idempotent pull left diff: %s", got)
	}

	run(t, consumer, bin, "remove", "fixture")
	if got := mustRead(t, filepath.Join(consumer, "pom.xml")); got != rootBody {
		t.Fatalf("root not byte-restored:\n%s", got)
	}
	if got := mustRead(t, filepath.Join(consumer, "unrelated.txt")); got != "preserve me exactly\n" {
		t.Fatalf("unrelated content changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(consumer, "deps", "fixture")); !os.IsNotExist(err) {
		t.Fatalf("checkout remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(consumer, "deps", "git-a2a.maven", "pom.xml")); !os.IsNotExist(err) {
		t.Fatalf("generated reactor POM remains: %v", err)
	}
}

func writeMavenUpstream(t *testing.T, root string, value int) {
	t.Helper()
	write(t, root, "pom.xml", `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>example.fixture</groupId>
  <artifactId>fixture-lib</artifactId>
  <version>1.0.0</version>
  <properties>
    <maven.compiler.release>17</maven.compiler.release>
    <project.build.sourceEncoding>UTF-8</project.build.sourceEncoding>
  </properties>
</project>
`)
	write(t, root, "src/main/java/example/fixture/Fixture.java", fmt.Sprintf("package example.fixture;\npublic final class Fixture { private Fixture() {} public static int value() { return %d; } }\n", value))
}

func assertMavenValue(t *testing.T, root, want string) {
	t.Helper()
	run(t, root, "mvn", "-B", "-ntp", "-DskipTests", "package")
	separator := string(os.PathListSeparator)
	classPath := strings.Join([]string{
		filepath.Join(root, "app", "target", "classes"),
		filepath.Join(root, "deps", "fixture", "target", "classes"),
	}, separator)
	if got := strings.TrimSpace(run(t, root, "java", "-cp", classPath, "example.consumer.App")); got != want {
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
