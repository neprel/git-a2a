package msbuild_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPublicCLIMSBuildCSharpLifecycle(t *testing.T) {
	testPublicCLIMSBuildLifecycle(t, "msbuild-csharp", "cs")
}

func TestPublicCLIMSBuildFSharpLifecycle(t *testing.T) {
	testPublicCLIMSBuildLifecycle(t, "msbuild-fsharp", "fs")
}

// testPublicCLIMSBuildLifecycle is gated because it executes the public CLI,
// local Git submodules, restore, compilation, and the resulting application.
func testPublicCLIMSBuildLifecycle(t *testing.T, variant, language string) {
	t.Helper()
	if os.Getenv("GITA2A_NATIVE_BUILD_IT") != variant {
		t.Skipf("set GITA2A_NATIVE_BUILD_IT=%s in the pinned .NET SDK environment", variant)
	}
	for _, tool := range []string{"git", "dotnet"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("required native tool %s: %v", tool, err)
		}
	}
	bin := msbuildCLI(t)

	upstream := t.TempDir()
	msbuildGitInit(t, upstream)
	msbuildWrite(t, upstream, "a2amodule.yml", "schema: 2\ncomponent:\n  id: native-"+variant+"\n  exports:\n    - adapter: nuget\n      name: NativeFixture\n      path: src\nagent:\n  card: https://agents.example/native-msbuild.json\n")
	msbuildWrite(t, upstream, ".gitignore", "bin/\nobj/\n")
	writeMSBuildUpstream(t, upstream, language, 41)
	msbuildGitCommit(t, upstream, "initial")

	consumer := t.TempDir()
	msbuildGitInit(t, consumer)
	rootProject := writeMSBuildConsumer(t, consumer, language)
	msbuildWrite(t, consumer, "unrelated.txt", "preserve me exactly\n")
	msbuildRun(t, consumer, bin, "init", "--id", "consumer")
	msbuildRun(t, consumer, bin, "add", msbuildFileURL(upstream), "--name", "fixture", "--ref", "main")
	assertMSBuildBindings(t, consumer, variant)
	assertMSBuildValue(t, consumer, rootProject, "41|unrelated")
	msbuildGitCommit(t, consumer, "installed")

	fresh := filepath.Join(t.TempDir(), "fresh")
	msbuildRun(t, filepath.Dir(fresh), "git", "clone", consumer, fresh)
	msbuildRun(t, fresh, bin, "pull", "fixture")
	assertMSBuildValue(t, fresh, rootProject, "41|unrelated")

	if err := os.RemoveAll(filepath.Join(consumer, "deps", "fixture")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(consumer, "obj")); err != nil {
		t.Fatal(err)
	}
	msbuildRun(t, consumer, bin, "pull", "fixture")
	assertMSBuildValue(t, consumer, rootProject, "41|unrelated")

	// The project version remains 1.0.0: targeted pull must advance the Git
	// checkout and make the new implementation observable anyway.
	writeMSBuildUpstream(t, upstream, language, 42)
	msbuildGitCommit(t, upstream, "same-version update")
	msbuildRun(t, consumer, bin, "pull", "fixture")
	assertMSBuildValue(t, consumer, rootProject, "42|unrelated")
	msbuildGitCommit(t, consumer, "updated")
	msbuildRun(t, consumer, bin, "pull", "fixture")
	if got := strings.TrimSpace(msbuildRun(t, consumer, "git", "status", "--porcelain")); got != "" {
		t.Fatalf("idempotent targeted pull left a diff: %s", got)
	}

	msbuildRun(t, consumer, bin, "remove", "fixture")
	if got := msbuildRead(t, filepath.Join(consumer, rootProject)); got != msbuildConsumerProject(language) {
		t.Fatalf("consumer project was not byte-restored:\n%s", got)
	}
	if got := msbuildRead(t, filepath.Join(consumer, "unrelated.txt")); got != "preserve me exactly\n" {
		t.Fatalf("unrelated content changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(consumer, "deps", "git-a2a.targets")); !os.IsNotExist(err) {
		t.Fatalf("generated targets remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(consumer, "deps", "fixture")); !os.IsNotExist(err) {
		t.Fatalf("submodule checkout remains: %v", err)
	}
	msbuildRun(t, consumer, "dotnet", "build", filepath.Join("unrelated", "Unrelated."+language+"proj"), "--nologo", "--no-restore")
}

func msbuildCLI(t *testing.T) string {
	t.Helper()
	if bin := os.Getenv("GITA2A_CLI_BIN"); bin != "" {
		info, err := os.Stat(bin)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			t.Fatalf("GITA2A_CLI_BIN must name an executable file: %s: %v", bin, err)
		}
		return bin
	}
	_, here, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
	bin := filepath.Join(t.TempDir(), "git-a2a")
	msbuildRun(t, repo, "go", "build", "-o", bin, "./cmd/git-a2a")
	return bin
}

func writeMSBuildUpstream(t *testing.T, root, language string, value int) {
	t.Helper()
	project := `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
    <Version>1.0.0</Version>
  </PropertyGroup>
`
	if language == "fs" {
		project += "  <ItemGroup><Compile Include=\"Library.fs\" /></ItemGroup>\n"
		msbuildWrite(t, root, "src/Library.fs", fmt.Sprintf("namespace NativeFixture\nmodule Values =\n    let value () = %d\n", value))
	} else {
		msbuildWrite(t, root, "src/Fixture.cs", fmt.Sprintf("namespace NativeFixture; public static class Values { public static int Value() => %d; }\n", value))
	}
	project += "</Project>\n"
	msbuildWrite(t, root, "src/NativeFixture."+language+"proj", project)
}

func writeMSBuildConsumer(t *testing.T, root, language string) string {
	t.Helper()
	rootProject := "Consumer." + language + "proj"
	msbuildWrite(t, root, rootProject, msbuildConsumerProject(language))
	if language == "fs" {
		msbuildWrite(t, root, "Program.fs", "printf \"%d|%s\" (NativeFixture.Values.value()) (Unrelated.Values.value())\n")
		msbuildWrite(t, root, "unrelated/Unrelated.fsproj", `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup>
  <ItemGroup><Compile Include="Library.fs" /></ItemGroup>
</Project>
`)
		msbuildWrite(t, root, "unrelated/Library.fs", "namespace Unrelated\nmodule Values =\n    let value () = \"unrelated\"\n")
	} else {
		msbuildWrite(t, root, "Program.cs", "System.Console.Write($\"{NativeFixture.Values.Value()}|{Unrelated.Values.Value()}\");\n")
		msbuildWrite(t, root, "unrelated/Unrelated.csproj", `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup>
</Project>
`)
		msbuildWrite(t, root, "unrelated/Values.cs", "namespace Unrelated; public static class Values { public static string Value() => \"unrelated\"; }\n")
	}
	return rootProject
}

func msbuildConsumerProject(language string) string {
	compile := "    <Compile Include=\"Program." + language + "\" />\n"
	return `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net8.0</TargetFramework>
    <EnableDefaultCompileItems>false</EnableDefaultCompileItems>
  </PropertyGroup>
  <ItemGroup>
` + compile + `    <ProjectReference Include="unrelated/Unrelated.` + language + `proj" />
  </ItemGroup>
  <!-- unrelated-user-content -->
</Project>
`
}

func assertMSBuildBindings(t *testing.T, root, variant string) {
	t.Helper()
	body := msbuildRead(t, filepath.Join(root, "a2amodule.yml"))
	for _, want := range []string{"adapter: submodule", "adapter: nuget", "variant: " + variant} {
		if !strings.Contains(body, want) {
			t.Fatalf("manifest lacks %q:\n%s", want, body)
		}
	}
}

func assertMSBuildValue(t *testing.T, root, project, want string) {
	t.Helper()
	got := strings.TrimSpace(msbuildRun(t, root, "dotnet", "run", "--project", project, "--nologo"))
	if got != want {
		t.Fatalf("application output=%q want=%q", got, want)
	}
}

func msbuildGitInit(t *testing.T, root string) {
	t.Helper()
	msbuildRun(t, root, "git", "init", "-b", "main")
	msbuildRun(t, root, "git", "config", "user.email", "native@example.test")
	msbuildRun(t, root, "git", "config", "user.name", "Native")
}

func msbuildGitCommit(t *testing.T, root, message string) {
	t.Helper()
	msbuildRun(t, root, "git", "add", "-A")
	msbuildRun(t, root, "git", "commit", "-m", message)
}

func msbuildWrite(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func msbuildRead(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func msbuildFileURL(path string) string { return "file://" + filepath.ToSlash(path) }

func msbuildRun(t *testing.T, dir, command string, args ...string) string {
	t.Helper()
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_ALLOW_PROTOCOL=file", "DOTNET_CLI_TELEMETRY_OPTOUT=1", "DOTNET_ROLL_FORWARD=Major")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", command, strings.Join(args, " "), err, out)
	}
	return string(out)
}
