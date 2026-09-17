package gradle

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// This test is gated because it executes the public CLI, Git submodules, a JVM,
// and Gradle. Pinned native runners select one DSL with
// GITA2A_NATIVE_BUILD_IT=gradle-kts or gradle-groovy.
func TestPublicCLIGradleLifecycle(t *testing.T) {
	variant := os.Getenv("GITA2A_NATIVE_BUILD_IT")
	if variant != "gradle-kts" && variant != "gradle-groovy" {
		t.Skip("set GITA2A_NATIVE_BUILD_IT=gradle-kts or gradle-groovy in a pinned Gradle environment")
	}
	for _, tool := range []string{"git", "gradle", "java"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("required native tool %s: %v", tool, err)
		}
	}

	_, here, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
	bin := gradleCLIBinary(t, repo)

	upstream := t.TempDir()
	gitInitGradle(t, upstream)
	writeGradleFile(t, upstream, ".gitignore", ".gradle/\nbuild/\n")
	writeGradleFile(t, upstream, "a2amodule.yml", "schema: 2\ncomponent:\n  id: native-gradle\n  exports:\n    - adapter: maven\n      name: example.fixture:fixture-lib\nagent:\n  card: https://agents.example/native-gradle.json\n")
	writeGradleUpstream(t, upstream, variant, 41)
	gitCommitGradle(t, upstream, "initial")

	consumer := t.TempDir()
	gitInitGradle(t, consumer)
	writeGradleConsumer(t, consumer, variant)
	settingsName := gradleSettingsName(variant)
	originalSettings := readGradleFile(t, filepath.Join(consumer, settingsName))
	originalBuild := readGradleFile(t, filepath.Join(consumer, gradleBuildName(variant)))
	runGradleCLI(t, consumer, bin, "init", "--id", "consumer")
	runGradleCLI(t, consumer, bin, "add", gradleFileURL(upstream), "--name", "fixture", "--ref", "main")
	assertGradleBindings(t, consumer, variant)
	assertGradleAppValue(t, consumer, "fixture=41;unrelated=preserve")
	gitCommitGradle(t, consumer, "installed")

	fresh := filepath.Join(t.TempDir(), "fresh")
	runGradleCLI(t, filepath.Dir(fresh), "git", "clone", consumer, fresh)
	runGradleCLI(t, fresh, bin, "pull", "fixture")
	assertGradleAppValue(t, fresh, "fixture=41;unrelated=preserve")

	if err := os.RemoveAll(filepath.Join(consumer, "deps", "fixture")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{".gradle", "build", filepath.Join("unrelated", "build")} {
		if err := os.RemoveAll(filepath.Join(consumer, path)); err != nil {
			t.Fatal(err)
		}
	}
	runGradleCLI(t, consumer, bin, "pull", "fixture")
	assertGradleAppValue(t, consumer, "fixture=41;unrelated=preserve")

	writeGradleUpstream(t, upstream, variant, 42)
	gitCommitGradle(t, upstream, "same-version update")
	runGradleCLI(t, consumer, bin, "pull", "fixture")
	assertGradleAppValue(t, consumer, "fixture=42;unrelated=preserve")
	gitCommitGradle(t, consumer, "updated")
	runGradleCLI(t, consumer, bin, "pull", "fixture")
	if got := strings.TrimSpace(runGradleCLI(t, consumer, "git", "status", "--porcelain")); got != "" {
		t.Fatalf("idempotent targeted pull left diff: %s", got)
	}

	runGradleCLI(t, consumer, bin, "remove", "fixture")
	if got := readGradleFile(t, filepath.Join(consumer, settingsName)); got != originalSettings {
		t.Fatalf("settings not byte-restored:\n%s", got)
	}
	if got := readGradleFile(t, filepath.Join(consumer, gradleBuildName(variant))); got != originalBuild {
		t.Fatalf("unrelated root build content changed:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(consumer, "deps", "fixture")); !os.IsNotExist(err) {
		t.Fatalf("submodule checkout remains: %v", err)
	}
	generated := "deps/git-a2a.settings.gradle.kts"
	if variant == "gradle-groovy" {
		generated = "deps/git-a2a.settings.gradle"
	}
	if _, err := os.Stat(filepath.Join(consumer, filepath.FromSlash(generated))); !os.IsNotExist(err) {
		t.Fatalf("generated Gradle integration remains: %v", err)
	}
	assertUnrelatedGradleValue(t, consumer, "preserve")
}

func gradleCLIBinary(t *testing.T, repo string) string {
	t.Helper()
	if binary := os.Getenv("GITA2A_CLI_BIN"); binary != "" {
		info, err := os.Stat(binary)
		if err != nil || info.IsDir() {
			t.Fatalf("GITA2A_CLI_BIN %q is not an executable file: %v", binary, err)
		}
		return binary
	}
	bin := filepath.Join(t.TempDir(), "git-a2a")
	runGradleCLI(t, repo, "go", "build", "-o", bin, "./cmd/git-a2a")
	return bin
}

func writeGradleUpstream(t *testing.T, root, variant string, value int) {
	t.Helper()
	if variant == "gradle-kts" {
		writeGradleFile(t, root, "settings.gradle.kts", "rootProject.name = \"fixture-lib\"\n")
		writeGradleFile(t, root, "build.gradle.kts", "plugins { `java-library` }\ngroup = \"example.fixture\"\nversion = \"1.0.0\"\n")
	} else {
		writeGradleFile(t, root, "settings.gradle", "rootProject.name = 'fixture-lib'\n")
		writeGradleFile(t, root, "build.gradle", "plugins { id 'java-library' }\ngroup = 'example.fixture'\nversion = '1.0.0'\n")
	}
	writeGradleFile(t, root, "src/main/java/example/fixture/Fixture.java", fmt.Sprintf("package example.fixture;\npublic final class Fixture { public static int value() { return %d; } }\n", value))
}

func writeGradleConsumer(t *testing.T, root, variant string) {
	t.Helper()
	writeGradleFile(t, root, ".gitignore", ".gradle/\nbuild/\n**/build/\n")
	if variant == "gradle-kts" {
		writeGradleFile(t, root, "settings.gradle.kts", "rootProject.name = \"consumer\"\ninclude(\"unrelated\")\n")
		writeGradleFile(t, root, "build.gradle.kts", "plugins { application }\n\ndependencies {\n  implementation(project(\":unrelated\"))\n  implementation(\"example.fixture:fixture-lib:1.0.0\")\n}\n\napplication { mainClass.set(\"consumer.Main\") }\n")
		writeGradleFile(t, root, "unrelated/build.gradle.kts", "plugins { `java-library` }\n")
	} else {
		writeGradleFile(t, root, "settings.gradle", "rootProject.name = 'consumer'\ninclude 'unrelated'\n")
		writeGradleFile(t, root, "build.gradle", "plugins { id 'application' }\n\ndependencies {\n  implementation project(':unrelated')\n  implementation 'example.fixture:fixture-lib:1.0.0'\n}\n\napplication { mainClass = 'consumer.Main' }\n")
		writeGradleFile(t, root, "unrelated/build.gradle", "plugins { id 'java-library' }\n")
	}
	writeGradleFile(t, root, "src/main/java/consumer/Main.java", "package consumer;\nimport example.fixture.Fixture;\nimport unrelated.Unrelated;\npublic final class Main { public static void main(String[] args) { System.out.print(\"fixture=\" + Fixture.value() + \";unrelated=\" + Unrelated.value()); } }\n")
	writeGradleFile(t, root, "unrelated/src/main/java/unrelated/Unrelated.java", "package unrelated;\npublic final class Unrelated { public static String value() { return \"preserve\"; } public static void main(String[] args) { System.out.print(value()); } }\n")
}

func assertGradleAppValue(t *testing.T, root, want string) {
	t.Helper()
	got := runGradleCLI(t, root, "gradle", "--no-daemon", "--console=plain", "-q", "run")
	if !strings.Contains(got, want) {
		t.Fatalf("Gradle application output lacks %q:\n%s", want, got)
	}
}

func assertUnrelatedGradleValue(t *testing.T, root, want string) {
	t.Helper()
	runGradleCLI(t, root, "gradle", "--no-daemon", "--console=plain", "-q", ":unrelated:classes")
	classes := filepath.Join(root, "unrelated", "build", "classes", "java", "main")
	got := strings.TrimSpace(runGradleCLI(t, root, "java", "-cp", classes, "unrelated.Unrelated"))
	if got != want {
		t.Fatalf("unrelated Gradle dependency value=%q want=%q", got, want)
	}
}

func assertGradleBindings(t *testing.T, root, variant string) {
	t.Helper()
	body := readGradleFile(t, filepath.Join(root, "a2amodule.yml"))
	for _, want := range []string{"adapter: submodule", "variant: git", "adapter: maven", "variant: " + variant} {
		if !strings.Contains(body, want) {
			t.Fatalf("manifest lacks %q:\n%s", want, body)
		}
	}
}

func gradleSettingsName(variant string) string {
	if variant == "gradle-groovy" {
		return "settings.gradle"
	}
	return "settings.gradle.kts"
}

func gradleBuildName(variant string) string {
	if variant == "gradle-groovy" {
		return "build.gradle"
	}
	return "build.gradle.kts"
}

func gitInitGradle(t *testing.T, root string) {
	t.Helper()
	runGradleCLI(t, root, "git", "init", "-b", "main")
	runGradleCLI(t, root, "git", "config", "user.email", "native@example.test")
	runGradleCLI(t, root, "git", "config", "user.name", "Native")
}

func gitCommitGradle(t *testing.T, root, message string) {
	t.Helper()
	runGradleCLI(t, root, "git", "add", "-A")
	runGradleCLI(t, root, "git", "commit", "-m", message)
}

func writeGradleFile(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readGradleFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func gradleFileURL(path string) string {
	slashed := filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		return "file:///" + strings.TrimPrefix(slashed, "/")
	}
	return "file://" + slashed
}

func runGradleCLI(t *testing.T, dir, command string, args ...string) string {
	t.Helper()
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_ALLOW_PROTOCOL=file")
	if os.Getenv("GRADLE_OPTS") == "" {
		cmd.Env = append(cmd.Env, "GRADLE_OPTS=-Dorg.gradle.jvmargs=-Xmx256m -Dorg.gradle.daemon=false")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", command, strings.Join(args, " "), err, out)
	}
	return string(out)
}
