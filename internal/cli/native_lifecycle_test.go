package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPublicCLINPMLifecycleMaterializesRepairsUpdatesAndRemoves(t *testing.T) {
	requireExecutable(t, "git")
	requireExecutable(t, "npm")
	requireExecutable(t, "node")
	bin := buildPublicCLI(t)
	upstream := newNativeRemote(t, map[string]string{
		"a2amodule.yml": "schema: 2\ncomponent:\n  id: fixture-lib\n  exports:\n    - adapter: npm\n      name: fixture-lib\nagent:\n  card: https://agents.example/fixture-lib.json\n",
		"package.json":  "{\"name\":\"fixture-lib\",\"version\":\"1.0.0\",\"main\":\"index.js\"}\n",
		"index.js":      "module.exports = { value: 'v1' };\n",
	})
	consumer := t.TempDir()
	mustWriteNative(t, filepath.Join(consumer, "unrelated", "package.json"), "{\"name\":\"unrelated\",\"version\":\"1.0.0\"}\n")
	mustWriteNative(t, filepath.Join(consumer, "package.json"), "{\"name\":\"consumer\",\"private\":true,\"dependencies\":{\"unrelated\":\"file:./unrelated\"}}\n")
	runNative(t, consumer, bin, "init", "--id", "consumer")
	runNative(t, consumer, bin, "add", fileURL(upstream), "--name", "fixture", "--ref", "main")
	assertNodeValue(t, consumer, "v1")
	if _, err := os.Stat(filepath.Join(consumer, "package-lock.json")); err != nil {
		t.Fatalf("npm native lock was not created: %v", err)
	}

	if err := os.RemoveAll(filepath.Join(consumer, "node_modules")); err != nil {
		t.Fatal(err)
	}
	runNative(t, consumer, bin, "pull", "fixture")
	assertNodeValue(t, consumer, "v1")

	gitNative(t, consumer, "init", "-b", "main")
	gitNative(t, consumer, "config", "user.email", "consumer@example.test")
	gitNative(t, consumer, "config", "user.name", "Consumer Test")
	gitNative(t, consumer, "add", ".gitignore", "a2amodule.yml", "a2amodule.lock", "package.json", "package-lock.json", "unrelated")
	gitNative(t, consumer, "commit", "-m", "installed dependency")
	fresh := filepath.Join(t.TempDir(), "fresh")
	runNative(t, filepath.Dir(fresh), "git", "clone", consumer, fresh)
	runNative(t, fresh, bin, "pull", "fixture")
	assertNodeValue(t, fresh, "v1")

	mustWriteNative(t, filepath.Join(upstream, "index.js"), "module.exports = { value: 'v2' };\n")
	gitNative(t, upstream, "add", "index.js")
	gitNative(t, upstream, "commit", "-m", "v2")
	runNative(t, consumer, bin, "pull", "fixture")
	assertNodeValue(t, consumer, "v2")

	runNative(t, consumer, bin, "remove", "fixture")
	packageBody := readJSONMap(t, filepath.Join(consumer, "package.json"))
	dependencies, _ := packageBody["dependencies"].(map[string]any)
	if _, exists := dependencies["fixture-lib"]; exists || dependencies["unrelated"] != "file:./unrelated" {
		t.Fatalf("package dependencies after remove: %#v", dependencies)
	}
	lockBody := readJSONMap(t, filepath.Join(consumer, "package-lock.json"))
	rootPackage := lockBody["packages"].(map[string]any)[""].(map[string]any)
	rootDependencies, _ := rootPackage["dependencies"].(map[string]any)
	if _, exists := rootDependencies["fixture-lib"]; exists {
		t.Fatalf("root package-lock dependency survived remove: %#v", rootDependencies)
	}
	if _, err := os.Stat(filepath.Join(consumer, "node_modules", "fixture-lib")); !os.IsNotExist(err) {
		t.Fatalf("installed package survived remove: %v", err)
	}
}

func TestPublicCLIUVLifecycleMaterializesAndRemoves(t *testing.T) {
	requireExecutable(t, "git")
	requireExecutable(t, "uv")
	bin := buildPublicCLI(t)
	upstream := newNativeRemote(t, map[string]string{
		"a2amodule.yml":          "schema: 2\ncomponent:\n  id: fixture-py\n  exports:\n    - adapter: pypi\n      name: fixture-py\nagent:\n  card: https://agents.example/fixture-py.json\n",
		"pyproject.toml":         "[project]\nname = \"fixture-py\"\nversion = \"1.0.0\"\n[build-system]\nrequires = [\"setuptools\"]\nbuild-backend = \"setuptools.build_meta\"\n",
		"fixture_py/__init__.py": "VALUE = 'v1'\n",
	})
	consumer := t.TempDir()
	mustWriteNative(t, filepath.Join(consumer, "pyproject.toml"), "[project]\nname = \"consumer\"\nversion = \"0.0.0\"\ndependencies = []\n\n[tool.uv]\n")
	runNative(t, consumer, bin, "init", "--id", "consumer")
	runNative(t, consumer, bin, "add", fileURL(upstream), "--name", "fixture", "--ref", "main")
	assertPythonValue(t, consumer, "v1")
	if err := os.RemoveAll(filepath.Join(consumer, ".venv")); err != nil {
		t.Fatal(err)
	}
	runNative(t, consumer, bin, "pull", "fixture")
	assertPythonValue(t, consumer, "v1")
	runNative(t, consumer, bin, "remove", "fixture")
	python := venvPython(consumer)
	cmd := exec.Command(python, "-c", "import fixture_py")
	cmd.Dir = consumer
	if err := cmd.Run(); err == nil {
		t.Fatal("Python package remained importable after remove")
	}
}

func buildPublicCLI(t *testing.T) string {
	t.Helper()
	if binary := os.Getenv("GITA2A_CLI_BIN"); binary != "" {
		if info, err := os.Stat(binary); err != nil || info.IsDir() {
			t.Fatalf("GITA2A_CLI_BIN %q is not an executable file: %v", binary, err)
		}
		return binary
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	bin := filepath.Join(t.TempDir(), "git-a2a")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/git-a2a")
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build public CLI: %v\n%s", err, output)
	}
	return bin
}

func newNativeRemote(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	gitNative(t, root, "init", "-b", "main")
	gitNative(t, root, "config", "user.email", "native@example.test")
	gitNative(t, root, "config", "user.name", "Native Test")
	for name, body := range files {
		mustWriteNative(t, filepath.Join(root, name), body)
	}
	gitNative(t, root, "add", ".")
	gitNative(t, root, "commit", "-m", "initial")
	return root
}

func mustWriteNative(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runNative(t *testing.T, dir, command string, args ...string) string {
	t.Helper()
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "NPM_CONFIG_UPDATE_NOTIFIER=false")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", command, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func gitNative(t *testing.T, dir string, args ...string) {
	t.Helper()
	runNative(t, dir, "git", args...)
}

func fileURL(path string) string {
	slashed := filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		return "file:///" + slashed
	}
	return "file://" + slashed
}

func assertNodeValue(t *testing.T, root, want string) {
	t.Helper()
	got := strings.TrimSpace(runNative(t, root, "node", "-e", "process.stdout.write(require('fixture-lib').value)"))
	if got != want {
		t.Fatalf("required npm package value = %q, want %q", got, want)
	}
}

func assertPythonValue(t *testing.T, root, want string) {
	t.Helper()
	python := venvPython(root)
	got := strings.TrimSpace(runNative(t, root, python, "-c", "import fixture_py; print(fixture_py.VALUE)"))
	if got != want {
		t.Fatalf("imported Python package value = %q, want %q", got, want)
	}
}

func venvPython(root string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(root, ".venv", "Scripts", "python.exe")
	}
	return filepath.Join(root, ".venv", "bin", "python")
}

func readJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err = json.Unmarshal(body, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func requireExecutable(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skip(fmt.Sprintf("%s unavailable", name))
	}
}
