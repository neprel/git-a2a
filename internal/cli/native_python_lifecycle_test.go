package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestPublicCLIPythonLifecycle is intentionally an end-to-end test. It must be
// run with the real public binary and real package managers; editor-only tests
// cannot establish that a Python dependency is importable at the locked Git
// revision.
func TestPublicCLIPythonLifecycle(t *testing.T) {
	requireNativePythonExecutable(t, "git")
	bin := buildPublicCLI(t)
	for _, variant := range []string{"uv", "poetry", "pdm", "pep621"} {
		variant := variant
		t.Run(variant, func(t *testing.T) {
			if !nativePythonCaseEnabled(variant) {
				t.Skipf("filtered by GITA2A_NATIVE_CASE=%q", os.Getenv("GITA2A_NATIVE_CASE"))
			}
			tool := variant
			if variant == "pep621" {
				requireNativePythonExecutable(t, "python3")
				tool = "pip"
			}
			requireNativePythonExecutable(t, tool)
			runPublicPythonLifecycle(t, bin, variant)
		})
	}
}

type nativePythonProject struct {
	variant      string
	root         string
	env          []string
	externalBase string
	lockFile     string
}

func runPublicPythonLifecycle(t *testing.T, bin, variant string) {
	t.Helper()
	upstream := newPythonNativeRemote(t, "v1", "charset-normalizer==3.4.3")
	source := fileURL(upstream)
	if variant == "pdm" {
		// PDM 2.26 interprets a git+file direct requirement as an SSH host named
		// "file". A loopback git daemon keeps the fixture local while exercising
		// PDM's real VCS resolver.
		source = servePythonNativeGit(t, upstream)
	}
	project := newNativePythonProject(t, variant)
	project.bootstrap(t)
	project.run(t, bin, "init", "--id", "consumer")
	project.run(t, bin, "add", source, "--name", "fixture", "--ref", "main")
	project.assertUsable(t, "v1", gitHead(t, upstream))
	project.assertExternalEnvironment(t)

	// list is an offline observation. In particular it must not rewrite a
	// manager lock, project declaration, or installed direct_url metadata.
	beforeList := project.stateDigest(t)
	out := project.run(t, bin, "list", "fixture", "--json")
	if !strings.Contains(out, `"name": "fixture"`) && !strings.Contains(out, `"name":"fixture"`) {
		t.Fatalf("list output does not contain fixture: %s", out)
	}
	if after := project.stateDigest(t); after != beforeList {
		t.Fatalf("list mutated Python project state: before %s, after %s", beforeList, after)
	}

	// Commit only reproducible project inputs. Environments and .git-a2a are
	// deliberately absent from the fresh clone.
	gitNative(t, project.root, "init", "-b", "main")
	gitNative(t, project.root, "config", "user.email", "consumer@example.test")
	gitNative(t, project.root, "config", "user.name", "Consumer Test")
	tracked := []string{".gitignore", "a2amodule.yml", "a2amodule.lock", "pyproject.toml"}
	if project.lockFile != "" {
		tracked = append(tracked, project.lockFile)
	}
	gitNative(t, project.root, append([]string{"add"}, tracked...)...)
	gitNative(t, project.root, "commit", "-m", "installed Python dependency")
	fresh := project.freshClone(t)
	fresh.run(t, bin, "pull", "fixture")
	fresh.assertUsable(t, "v1", gitHead(t, upstream))

	// Losing the complete project environment must be repaired even though the
	// requested branch still resolves to the same commit.
	project.removeEnvironment(t)
	project.run(t, bin, "pull", "fixture")
	project.assertUsable(t, "v1", gitHead(t, upstream))

	// The package version remains 1.0.0: the only revision discriminator is the
	// Git commit recorded in direct_url.json and the git-a2a lock.
	writePythonUpstream(t, upstream, "v2", "charset-normalizer==3.4.3")
	gitNative(t, upstream, "add", "pyproject.toml", "fixture_py/__init__.py")
	gitNative(t, upstream, "commit", "-m", "same version, second revision")
	v2Commit := gitHead(t, upstream)
	project.run(t, bin, "pull", "fixture")
	project.assertUsable(t, "v2", v2Commit)

	// Pulling the already-applied revision may repair materialization, but must
	// not introduce declaration or lock churn.
	beforeIdempotent := project.manifestDigest(t)
	project.run(t, bin, "pull", "fixture")
	if after := project.manifestDigest(t); after != beforeIdempotent {
		t.Fatalf("idempotent pull changed declarations or locks: before %s, after %s", beforeIdempotent, after)
	}

	// A real native resolution failure after an upstream advance must not be
	// persisted as a successful git-a2a revision and owned files must roll back.
	beforeFailure := project.manifestDigest(t)
	writePythonUpstream(t, upstream, "v3", "git-a2a-no-such-package-for-native-test==0.0.0")
	gitNative(t, upstream, "add", "pyproject.toml", "fixture_py/__init__.py")
	gitNative(t, upstream, "commit", "-m", "unresolvable dependency")
	failed := project.runFailure(t, bin, "pull", "fixture")
	if strings.TrimSpace(failed) == "" {
		t.Fatal("native manager failure returned no diagnostic")
	}
	if after := project.manifestDigest(t); after != beforeFailure {
		t.Fatalf("failed pull did not restore declarations/locks: before %s, after %s\n%s", beforeFailure, after, failed)
	}
	project.assertUsable(t, "v2", v2Commit)

	project.run(t, bin, "remove", "fixture")
	if body, err := os.ReadFile(filepath.Join(project.root, "pyproject.toml")); err != nil {
		t.Fatal(err)
	} else if strings.Contains(strings.ToLower(string(body)), "fixture-py") {
		t.Fatalf("root dependency survived remove:\n%s", body)
	}
	project.assertRemovedAndUnrelatedPreserved(t)
}

func newNativePythonProject(t *testing.T, variant string) nativePythonProject {
	t.Helper()
	root := t.TempDir()
	toolState := t.TempDir()
	env := nativePythonEnvironment(toolState)
	p := nativePythonProject{variant: variant, root: root, env: env}
	switch variant {
	case "uv":
		p.lockFile = "uv.lock"
		mustWriteNative(t, filepath.Join(root, "pyproject.toml"), `[project]
name = "consumer"
version = "0.0.0"
dependencies = ["idna==3.10"]

[tool.uv]
`)
	case "poetry":
		p.lockFile = "poetry.lock"
		p.externalBase = filepath.Join(toolState, "poetry-venvs")
		p.env = withNativeEnv(p.env,
			"POETRY_VIRTUALENVS_IN_PROJECT=false",
			"POETRY_VIRTUALENVS_PATH="+p.externalBase,
		)
		mustWriteNative(t, filepath.Join(root, "pyproject.toml"), `[tool.poetry]
name = "consumer"
version = "0.0.0"
description = "native lifecycle fixture"
authors = ["git-a2a tests <native@example.test>"]
package-mode = false

[tool.poetry.dependencies]
python = ">=3.11,<4.0"
idna = "3.10"

[build-system]
requires = ["poetry-core"]
build-backend = "poetry.core.masonry.api"
`)
	case "pdm":
		p.lockFile = "pdm.lock"
		p.externalBase = filepath.Join(toolState, "pdm-environment")
		mustWriteNative(t, filepath.Join(root, "pyproject.toml"), `[project]
name = "consumer"
version = "0.0.0"
requires-python = ">=3.11"
dependencies = ["idna==3.10"]

[tool.pdm]
distribution = false
`)
	case "pep621":
		mustWriteNative(t, filepath.Join(root, "pyproject.toml"), `[project]
name = "consumer"
version = "0.0.0"
requires-python = ">=3.11"
dependencies = ["idna==3.10"]
`)
	default:
		t.Fatalf("unknown Python variant %q", variant)
	}
	return p
}

func (p nativePythonProject) bootstrap(t *testing.T) {
	t.Helper()
	switch p.variant {
	case "uv":
		p.run(t, "uv", "lock")
		p.run(t, "uv", "sync", "--no-install-project")
	case "poetry":
		p.run(t, "poetry", "lock")
		p.run(t, "poetry", "install", "--no-root", "--no-interaction")
	case "pdm":
		python := pythonExecutableName()
		p.run(t, python, "-m", "venv", p.externalBase)
		p.run(t, "pdm", "use", "-f", pythonInEnvironment(p.externalBase))
		p.run(t, "pdm", "lock")
		p.run(t, "pdm", "sync", "--no-self")
	case "pep621":
		p.run(t, pythonExecutableName(), "-m", "venv", filepath.Join(p.root, ".venv"))
		p.run(t, pythonInEnvironment(filepath.Join(p.root, ".venv")), "-m", "pip", "install", "--disable-pip-version-check", "idna==3.10")
	}
}

func (p nativePythonProject) freshClone(t *testing.T) nativePythonProject {
	t.Helper()
	fresh := p
	fresh.root = filepath.Join(t.TempDir(), "fresh")
	fresh.externalBase = ""
	if p.variant == "poetry" {
		fresh.externalBase = filepath.Join(t.TempDir(), "poetry-venvs")
		fresh.env = withNativeEnv(p.env, "POETRY_VIRTUALENVS_PATH="+fresh.externalBase)
	}
	if p.variant == "pdm" {
		fresh.externalBase = filepath.Join(t.TempDir(), "pdm-environment")
	}
	gitNative(t, filepath.Dir(fresh.root), "clone", p.root, fresh.root)
	if p.variant == "pdm" {
		fresh.run(t, pythonExecutableName(), "-m", "venv", fresh.externalBase)
		fresh.run(t, "pdm", "use", "-f", pythonInEnvironment(fresh.externalBase))
	}
	return fresh
}

func (p nativePythonProject) python(t *testing.T) string {
	t.Helper()
	switch p.variant {
	case "uv", "pep621":
		return pythonInEnvironment(filepath.Join(p.root, ".venv"))
	case "poetry":
		return strings.TrimSpace(p.run(t, "poetry", "env", "info", "--executable"))
	case "pdm":
		return strings.TrimSpace(p.run(t, "pdm", "info", "--python"))
	default:
		t.Fatalf("unknown Python variant %q", p.variant)
		return ""
	}
}

func (p nativePythonProject) environmentRoot(t *testing.T) string {
	t.Helper()
	if p.variant == "poetry" {
		return strings.TrimSpace(p.run(t, "poetry", "env", "info", "--path"))
	}
	python := p.python(t)
	return filepath.Dir(filepath.Dir(python))
}

func (p nativePythonProject) assertExternalEnvironment(t *testing.T) {
	t.Helper()
	if p.variant != "poetry" && p.variant != "pdm" {
		return
	}
	root := p.environmentRoot(t)
	want := canonicalNativePath(t, p.externalBase)
	root = canonicalNativePath(t, root)
	var err error
	rel, err := filepath.Rel(want, root)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("%s environment %q is not below isolated external root %q", p.variant, root, want)
	}
	if _, err := os.Stat(filepath.Join(p.root, ".venv")); !os.IsNotExist(err) {
		t.Fatalf("%s unexpectedly created consumer/.venv: %v", p.variant, err)
	}
}

func (p nativePythonProject) removeEnvironment(t *testing.T) {
	t.Helper()
	root := p.environmentRoot(t)
	if p.variant == "pdm" || p.variant == "poetry" {
		base := canonicalNativePath(t, p.externalBase)
		absolute := canonicalNativePath(t, root)
		var err error
		rel, err := filepath.Rel(base, absolute)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Fatalf("refusing to remove non-isolated environment %q (base %q)", absolute, base)
		}
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
}

func canonicalNativePath(t *testing.T, path string) string {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(absolute); resolveErr == nil {
		return resolved
	}
	return absolute
}

func (p nativePythonProject) assertUsable(t *testing.T, value, commit string) {
	t.Helper()
	python := p.python(t)
	want := value + ":3.4.3"
	got := strings.TrimSpace(p.run(t, python, "-c", "import fixture_py; print(fixture_py.VALUE)"))
	if got != want {
		t.Fatalf("%s import value = %q, want %q", p.variant, got, want)
	}
	script := `import importlib.metadata, json
d = importlib.metadata.distribution("fixture-py")
print(d.read_text("direct_url.json") or "")`
	raw := strings.TrimSpace(p.run(t, python, "-c", script))
	var direct struct {
		VCSInfo struct {
			CommitID string `json:"commit_id"`
		} `json:"vcs_info"`
	}
	if err := json.Unmarshal([]byte(raw), &direct); err != nil {
		t.Fatalf("parse %s direct_url.json %q: %v", p.variant, raw, err)
	}
	if direct.VCSInfo.CommitID != commit {
		t.Fatalf("%s installed commit = %q, want %q (direct_url=%s)", p.variant, direct.VCSInfo.CommitID, commit, raw)
	}
}

func (p nativePythonProject) assertRemovedAndUnrelatedPreserved(t *testing.T) {
	t.Helper()
	python := p.python(t)
	if output, err := p.command(python, "-c", "import fixture_py").CombinedOutput(); err == nil {
		t.Fatalf("%s root package remained importable after remove: %s", p.variant, output)
	}
	got := strings.TrimSpace(p.run(t, python, "-c", "import idna; print(idna.__version__)"))
	if got != "3.10" {
		t.Fatalf("%s unrelated idna = %q, want 3.10", p.variant, got)
	}
	if p.lockFile != "" {
		body, err := os.ReadFile(filepath.Join(p.root, p.lockFile))
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(string(body))
		if strings.Contains(lower, "fixture-py") {
			t.Fatalf("%s root package survived in %s after remove", p.variant, p.lockFile)
		}
		if !strings.Contains(lower, "idna") || !strings.Contains(lower, "3.10") {
			t.Fatalf("%s unrelated idna pin is absent from %s", p.variant, p.lockFile)
		}
	}
}

func (p nativePythonProject) manifestDigest(t *testing.T) string {
	t.Helper()
	files := []string{"pyproject.toml", "a2amodule.yml", "a2amodule.lock"}
	if p.lockFile != "" {
		files = append(files, p.lockFile)
	}
	return digestFiles(t, p.root, files)
}

func (p nativePythonProject) stateDigest(t *testing.T) string {
	t.Helper()
	files := []string{"pyproject.toml", "a2amodule.yml", "a2amodule.lock"}
	if p.lockFile != "" {
		files = append(files, p.lockFile)
	}
	python := p.python(t)
	script := `import importlib.metadata
d = importlib.metadata.distribution("fixture-py")
print(d.locate_file("fixture_py/__init__.py"))
print(d.locate_file(d._path / "direct_url.json"))`
	for _, path := range strings.Split(strings.TrimSpace(p.run(t, python, "-c", script)), "\n") {
		path = strings.TrimSpace(path)
		if filepath.IsAbs(path) {
			files = append(files, path)
		}
	}
	return digestFiles(t, p.root, files)
}

func (p nativePythonProject) run(t *testing.T, command string, args ...string) string {
	t.Helper()
	cmd := p.command(command, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s (%s): %v\n%s", command, strings.Join(args, " "), p.variant, err, output)
	}
	return string(output)
}

func (p nativePythonProject) runFailure(t *testing.T, command string, args ...string) string {
	t.Helper()
	cmd := p.command(command, args...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("%s %s (%s) unexpectedly succeeded\n%s", command, strings.Join(args, " "), p.variant, output)
	}
	return string(output)
}

func (p nativePythonProject) command(command string, args ...string) *exec.Cmd {
	cmd := exec.Command(command, args...)
	cmd.Dir = p.root
	cmd.Env = p.env
	return cmd
}

func newPythonNativeRemote(t *testing.T, value, dependency string) string {
	t.Helper()
	root := t.TempDir()
	gitNative(t, root, "init", "-b", "main")
	gitNative(t, root, "config", "user.email", "native@example.test")
	gitNative(t, root, "config", "user.name", "Native Test")
	writePythonUpstream(t, root, value, dependency)
	mustWriteNative(t, filepath.Join(root, "a2amodule.yml"), "schema: 2\ncomponent:\n  id: fixture-py\n  exports:\n    - adapter: pypi\n      name: fixture-py\nagent:\n  card: https://agents.example/fixture-py.json\n")
	gitNative(t, root, "add", ".")
	gitNative(t, root, "commit", "-m", "initial")
	return root
}

func servePythonNativeGit(t *testing.T, repository string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	base := filepath.Dir(repository)
	command := exec.Command("git", "daemon", "--reuseaddr", "--export-all", "--listen=127.0.0.1", fmt.Sprintf("--port=%d", port), "--base-path="+base, base)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _, _ = command.Process.Wait() })
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
	return fmt.Sprintf("git://%s/%s", address, filepath.Base(repository))
}

func writePythonUpstream(t *testing.T, root, value, dependency string) {
	t.Helper()
	mustWriteNative(t, filepath.Join(root, "pyproject.toml"), fmt.Sprintf(`[project]
name = "fixture-py"
version = "1.0.0"
dependencies = [%q]

[build-system]
requires = ["setuptools>=68"]
build-backend = "setuptools.build_meta"
`, dependency))
	mustWriteNative(t, filepath.Join(root, "fixture_py", "__init__.py"), fmt.Sprintf("import charset_normalizer\nVALUE = %q + ':' + charset_normalizer.__version__\n", value))
}

func gitHead(t *testing.T, root string) string {
	t.Helper()
	return strings.TrimSpace(runNative(t, root, "git", "rev-parse", "HEAD"))
}

func nativePythonEnvironment(toolState string) []string {
	return withNativeEnv(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"PIP_DISABLE_PIP_VERSION_CHECK=1",
		"UV_CACHE_DIR="+filepath.Join(toolState, "uv-cache"),
		"POETRY_CACHE_DIR="+filepath.Join(toolState, "poetry-cache"),
		"PDM_HOME="+filepath.Join(toolState, "pdm-home"),
		"PDM_CACHE_DIR="+filepath.Join(toolState, "pdm-cache"),
		"PDM_CHECK_UPDATE=false",
	)
}

func withNativeEnv(base []string, replacements ...string) []string {
	values := make(map[string]string, len(base)+len(replacements))
	for _, item := range append(append([]string(nil), base...), replacements...) {
		key, _, ok := strings.Cut(item, "=")
		if ok {
			values[key] = item
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, values[key])
	}
	return out
}

func digestFiles(t *testing.T, root string, paths []string) string {
	t.Helper()
	h := sha256.New()
	for _, path := range paths {
		resolved := path
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(root, resolved)
		}
		body, err := os.ReadFile(resolved)
		if err != nil {
			t.Fatalf("read digest input %s: %v", resolved, err)
		}
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00", path, len(body))
		_, _ = h.Write(body)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func nativePythonCaseEnabled(variant string) bool {
	filter := strings.TrimSpace(os.Getenv("GITA2A_NATIVE_CASE"))
	if filter == "" || filter == "python" || filter == "all" {
		return true
	}
	for _, item := range strings.Split(filter, ",") {
		item = strings.TrimSpace(item)
		if item == variant || item == "python/"+variant || item == "python:"+variant {
			return true
		}
	}
	return false
}

func requireNativePythonExecutable(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err == nil {
		return
	}
	if os.Getenv("GITA2A_NATIVE_REQUIRED") == "1" {
		t.Fatalf("required native Python tool %s is unavailable", name)
	}
	t.Skipf("%s unavailable", name)
}

func pythonExecutableName() string {
	if runtime.GOOS == "windows" {
		return "python"
	}
	return "python3"
}

func pythonInEnvironment(root string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(root, "Scripts", "python.exe")
	}
	return filepath.Join(root, "bin", "python")
}
