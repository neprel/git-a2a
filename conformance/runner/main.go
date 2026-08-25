package main

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

type httpFixture struct {
	Status          int               `json:"status"`
	ResponseBody    string            `json:"responseBody"`
	ResponseHeaders map[string]string `json:"responseHeaders"`
	Method          string            `json:"method"`
	Path            string            `json:"path"`
	Body            string            `json:"body"`
}

type observedRequest struct {
	method string
	path   string
	body   string
}

type gitFixture struct {
	Files        map[string]string `json:"files"`
	InitWorktree bool              `json:"initWorktree"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	root, err := repositoryRoot()
	if err != nil {
		return err
	}
	casesRoot := filepath.Join(root, "conformance", "cases")
	entries, err := os.ReadDir(casesRoot)
	if err != nil {
		return err
	}
	selected := map[string]bool{}
	list := false
	for _, arg := range os.Args[1:] {
		if arg == "--list" {
			list = true
		} else {
			selected[arg] = true
		}
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && regexp.MustCompile(`^[0-9]{3}-`).MatchString(entry.Name()) {
			if len(selected) == 0 || selected[entry.Name()] {
				names = append(names, entry.Name())
			}
		}
	}
	sort.Strings(names)
	if list {
		for _, name := range names {
			fmt.Println(name)
		}
		return nil
	}
	if len(names) == 0 {
		return errors.New("conformance: no cases selected")
	}
	binary := os.Getenv("CONFORMANCE_BIN")
	if binary == "" {
		return errors.New("conformance: set CONFORMANCE_BIN to the implementation executable")
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		return err
	}
	failed, skipped := 0, 0
	for _, name := range names {
		caseDir := filepath.Join(casesRoot, name)
		if skipCase(caseDir) {
			fmt.Printf("SKIP %s\n", name)
			skipped++
			continue
		}
		if err := runCase(root, caseDir, binary); err != nil {
			fmt.Printf("FAIL %s: %v\n", name, err)
			failed++
		} else {
			fmt.Printf("PASS %s\n", name)
		}
	}
	fmt.Printf("conformance: %d passed, %d skipped, %d failed\n", len(names)-failed-skipped, skipped, failed)
	if failed != 0 {
		return fmt.Errorf("conformance: %d case(s) failed", failed)
	}
	return nil
}

func runCase(corpusRoot, caseDir, binary string) error {
	work, err := os.MkdirTemp("", "git-a2a-conformance-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	if err := copyTree(filepath.Join(caseDir, "manifest"), work); err != nil {
		return fmt.Errorf("copy manifest: %w", err)
	}
	if _, err := os.Stat(filepath.Join(caseDir, "cache")); err == nil {
		if err := copyTree(filepath.Join(caseDir, "cache"), filepath.Join(work, ".git-a2a", "cache")); err != nil {
			return fmt.Errorf("copy disposable cache fixture: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	var server *httptest.Server
	var observed *observedRequest
	fixture, fixtureErr := loadHTTPFixture(filepath.Join(caseDir, "http-fixture.json"))
	if fixtureErr != nil {
		return fixtureErr
	}
	replacements := map[string]string{"<ROOT>": work, "<CORPUS_ROOT>": corpusRoot}
	extraEnv := map[string]string{}
	if fixture != nil {
		observed = &observedRequest{}
		server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			observed.method, observed.path, observed.body = r.Method, r.URL.RequestURI(), string(body)
			for key, value := range fixture.ResponseHeaders {
				w.Header().Set(key, value)
			}
			status := fixture.Status
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			_, _ = io.WriteString(w, fixture.ResponseBody)
		}))
		defer server.Close()
		replacements["{{HTTP_URL}}"] = server.URL
		replacements["<HTTP_URL>"] = server.URL
		cert := server.Certificate()
		certPath := filepath.Join(work, ".conformance-ca.pem")
		if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), 0o600); err != nil {
			return err
		}
		if _, err := x509.ParseCertificate(cert.Raw); err != nil {
			return err
		}
		extraEnv["SSL_CERT_FILE"] = certPath
	}
	if err := startGitFixture(filepath.Join(caseDir, "git-fixture.json"), work, replacements); err != nil {
		return err
	}
	if err := replaceTree(work, replacements); err != nil {
		return err
	}

	if err := runFixture(caseDir, work, corpusRoot); err != nil {
		return err
	}
	args, err := readCommand(filepath.Join(caseDir, "command"), replacements)
	if err != nil {
		return err
	}
	env, err := caseEnvironment(filepath.Join(caseDir, "env.json"), replacements)
	if err != nil {
		return err
	}
	for key, value := range extraEnv {
		env[key] = value
	}
	command := exec.Command(binary, args...)
	command.Dir = work
	command.Env = mergeEnvironment(os.Environ(), env)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	runErr := command.Run()
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			return fmt.Errorf("start command: %w", runErr)
		}
		exitCode = exitErr.ExitCode()
	}
	wantExit, err := readExitCode(filepath.Join(caseDir, "expected", "exit-code"))
	if err != nil {
		return err
	}
	if exitCode != wantExit {
		return fmt.Errorf("exit=%d want=%d; stdout=%q stderr=%q", exitCode, wantExit, stdout.String(), stderr.String())
	}
	normalize := func(value string) string {
		value = strings.ReplaceAll(value, "\r\n", "\n")
		value = strings.ReplaceAll(value, work, "<ROOT>")
		value = strings.ReplaceAll(value, corpusRoot, "<CORPUS_ROOT>")
		if server != nil {
			value = strings.ReplaceAll(value, server.URL, "<HTTP_URL>")
		}
		return value
	}
	if err := matchPatterns(filepath.Join(caseDir, "expected", "stdout"), normalize(stdout.String())); err != nil {
		return fmt.Errorf("stdout: %w; got %q", err, normalize(stdout.String()))
	}
	if err := matchPatterns(filepath.Join(caseDir, "expected", "stderr"), normalize(stderr.String())); err != nil {
		return fmt.Errorf("stderr: %w; got %q", err, normalize(stderr.String()))
	}
	if fixture != nil {
		if observed.method != fixture.Method || observed.path != fixture.Path || observed.body != fixture.Body {
			return fmt.Errorf("HTTP request = %s %s %q, want %s %s %q", observed.method, observed.path, observed.body, fixture.Method, fixture.Path, fixture.Body)
		}
	}
	if err := compareExpectedFiles(filepath.Join(caseDir, "expected", "files"), work); err != nil {
		return err
	}
	if err := matchExpectedFilePatterns(filepath.Join(caseDir, "expected", "file-patterns.json"), work, replacements); err != nil {
		return err
	}
	return checkAbsent(filepath.Join(caseDir, "expected", "absent"), work)
}

func startGitFixture(path, work string, replacements map[string]string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var fixture gitFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		return fmt.Errorf("git-fixture.json: %w", err)
	}
	base := filepath.Join(work, ".git-a2a", ".conformance-git")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return err
	}
	source, bare := filepath.Join(base, "source"), filepath.Join(base, "acme-lib.git")
	if err := os.MkdirAll(source, 0o755); err != nil {
		return err
	}
	for name, content := range fixture.Files {
		target := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			return err
		}
	}
	runGit := func(dir string, args ...string) (string, error) {
		command := exec.Command("git", args...)
		command.Dir = dir
		command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(work, ".empty-gitconfig"))
		output, err := command.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, output)
		}
		return strings.TrimSpace(string(output)), nil
	}
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Acme Fixture"}, {"config", "user.email", "fixture@example.invalid"}, {"add", "."}, {"commit", "-m", "fixture"}} {
		if _, err := runGit(source, args...); err != nil {
			return err
		}
	}
	commit, err := runGit(source, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	tree, err := runGit(source, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return err
	}
	if _, err := runGit(base, "clone", "--bare", source, bare); err != nil {
		return err
	}
	if fixture.InitWorktree {
		if _, err := runGit(work, "init", "-b", "main"); err != nil {
			return err
		}
	}
	fixtureURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(bare)}).String()
	replacements["{{GIT_URL}}"] = fixtureURL
	replacements["{{GIT_COMMIT}}"] = commit
	replacements["{{GIT_TREE}}"] = tree
	return nil
}

func matchExpectedFilePatterns(path, root string, replacements map[string]string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var expected map[string][]string
	if err := json.Unmarshal(data, &expected); err != nil {
		return fmt.Errorf("file-patterns.json: %w", err)
	}
	for name, patterns := range expected {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return fmt.Errorf("expected file %s: %w", name, err)
		}
		for _, pattern := range patterns {
			for token, replacement := range replacements {
				pattern = strings.ReplaceAll(pattern, token, regexp.QuoteMeta(replacement))
			}
			matched, err := regexp.MatchString(pattern, string(body))
			if err != nil {
				return fmt.Errorf("expected file %s pattern %q: %w", name, pattern, err)
			}
			if !matched {
				return fmt.Errorf("expected file %s does not match %q; got %q", name, pattern, string(body))
			}
		}
	}
	return nil
}

func repositoryRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "conformance", "VERSION")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("conformance: repository root not found")
		}
		dir = parent
	}
}

func skipCase(caseDir string) bool {
	data, err := os.ReadFile(filepath.Join(caseDir, "platform"))
	if err != nil {
		return false
	}
	switch strings.TrimSpace(string(data)) {
	case "windows":
		return runtime.GOOS != "windows"
	case "posix":
		return runtime.GOOS == "windows"
	default:
		return false
	}
}

func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, info.Mode().Perm())
	})
}

func replaceTree(root string, replacements map[string]string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil || bytes.IndexByte(data, 0) >= 0 {
			return err
		}
		updated := string(data)
		for old, value := range replacements {
			updated = strings.ReplaceAll(updated, old, value)
		}
		if updated == string(data) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(path, []byte(updated), info.Mode().Perm())
	})
}

func readCommand(path string, replacements map[string]string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var args []string
	if err := json.Unmarshal(data, &args); err != nil {
		return nil, fmt.Errorf("command: %w", err)
	}
	for index := range args {
		for old, value := range replacements {
			args[index] = strings.ReplaceAll(args[index], old, value)
		}
	}
	return args, nil
}

func readExitCode(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("expected exit-code: %w", err)
	}
	return value, nil
}

func matchPatterns(path, output string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for number, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		negative := strings.HasPrefix(line, "!")
		if negative {
			line = strings.TrimPrefix(line, "!")
		}
		matched, err := regexp.MatchString(line, output)
		if err != nil {
			return fmt.Errorf("line %d: %w", number+1, err)
		}
		if matched == negative {
			return fmt.Errorf("line %d pattern %q expectation failed", number+1, line)
		}
	}
	return nil
}

func caseEnvironment(path string, replacements map[string]string) (map[string]string, error) {
	result := map[string]string{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	for key, value := range result {
		for old, replacement := range replacements {
			value = strings.ReplaceAll(value, old, replacement)
		}
		result[key] = value
	}
	return result, nil
}

func mergeEnvironment(base []string, additions map[string]string) []string {
	values := map[string]string{}
	for _, entry := range base {
		if key, value, ok := strings.Cut(entry, "="); ok {
			values[key] = value
		}
	}
	for key, value := range additions {
		if key == "PATH" && strings.HasSuffix(value, string(os.PathListSeparator)) {
			value += values["PATH"]
		}
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func loadHTTPFixture(path string) (*httpFixture, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var fixture httpFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		return nil, err
	}
	return &fixture, nil
}

func runFixture(caseDir, work, corpusRoot string) error {
	path := filepath.Join(caseDir, "fixture.sh")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if runtime.GOOS == "windows" {
		return errors.New("fixture.sh requires platform=posix")
	}
	command := exec.Command("sh", path)
	command.Dir = work
	command.Env = mergeEnvironment(os.Environ(), map[string]string{"ROOT": work, "CORPUS_ROOT": corpusRoot})
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("fixture.sh: %w: %s", err, output)
	}
	return nil
}

func compareExpectedFiles(expectedRoot, work string) error {
	if _, err := os.Stat(expectedRoot); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(expectedRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(expectedRoot, path)
		if err != nil {
			return err
		}
		want, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got, err := os.ReadFile(filepath.Join(work, rel))
		if err != nil {
			return fmt.Errorf("result %s: %w", rel, err)
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("result %s differs: got %q want %q", rel, got, want)
		}
		return nil
	})
}

func checkAbsent(path, work string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, err := os.Lstat(filepath.Join(work, filepath.FromSlash(line))); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("result %s must be absent", line)
		}
	}
	return nil
}
