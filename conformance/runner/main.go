package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

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
	entries, err := os.ReadDir(filepath.Join(root, "conformance", "cases"))
	if err != nil {
		return err
	}
	selected := map[string]bool{}
	listOnly := false
	for _, arg := range os.Args[1:] {
		if arg == "--list" {
			listOnly = true
		} else {
			selected[arg] = true
		}
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && regexp.MustCompile(`^[0-9]{3}-`).MatchString(entry.Name()) && (len(selected) == 0 || selected[entry.Name()]) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	if listOnly {
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
	if binary, err = filepath.Abs(binary); err != nil {
		return err
	}
	failed := 0
	for _, name := range names {
		if err = runCase(root, filepath.Join(root, "conformance", "cases", name), binary); err != nil {
			fmt.Printf("FAIL %s: %v\n", name, err)
			failed++
		} else {
			fmt.Printf("PASS %s\n", name)
		}
	}
	fmt.Printf("conformance: %d passed, %d failed\n", len(names)-failed, failed)
	if failed > 0 {
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
	if err = copyTree(filepath.Join(caseDir, "manifest"), work); err != nil {
		return fmt.Errorf("copy manifest: %w", err)
	}
	replacements := map[string]string{"<ROOT>": work, "<CORPUS_ROOT>": corpusRoot}
	if err = startGitFixture(filepath.Join(caseDir, "git-fixture.json"), work, replacements); err != nil {
		return err
	}
	if err = replaceTree(work, replacements); err != nil {
		return err
	}
	commands, err := readCommands(filepath.Join(caseDir, "command"), replacements)
	if err != nil {
		return err
	}
	env, err := caseEnvironment(filepath.Join(caseDir, "env.json"), replacements)
	if err != nil {
		return err
	}
	var stdout, stderr bytes.Buffer
	exitCode := 0
	for index, args := range commands {
		command := exec.Command(binary, args...)
		command.Dir = work
		command.Env = mergeEnvironment(os.Environ(), env)
		command.Stdout, command.Stderr = &stdout, &stderr
		if runErr := command.Run(); runErr != nil {
			var exitErr *exec.ExitError
			if !errors.As(runErr, &exitErr) {
				return fmt.Errorf("start command %d: %w", index+1, runErr)
			}
			exitCode = exitErr.ExitCode()
			break
		}
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
		return strings.ReplaceAll(value, corpusRoot, "<CORPUS_ROOT>")
	}
	if err = matchPatterns(filepath.Join(caseDir, "expected", "stdout"), normalize(stdout.String())); err != nil {
		return fmt.Errorf("stdout: %w; got %q", err, normalize(stdout.String()))
	}
	if err = matchPatterns(filepath.Join(caseDir, "expected", "stderr"), normalize(stderr.String())); err != nil {
		return fmt.Errorf("stderr: %w; got %q", err, normalize(stderr.String()))
	}
	if err = compareExpectedFiles(filepath.Join(caseDir, "expected", "files"), work); err != nil {
		return err
	}
	if err = matchExpectedFilePatterns(filepath.Join(caseDir, "expected", "file-patterns.json"), work, replacements); err != nil {
		return err
	}
	return checkAbsent(filepath.Join(caseDir, "expected", "absent"), work)
}

func startGitFixture(path, work string, replacements map[string]string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var fixture gitFixture
	if err = json.Unmarshal(data, &fixture); err != nil {
		return fmt.Errorf("git-fixture.json: %w", err)
	}
	base := filepath.Join(work, ".git-a2a", ".conformance-git")
	source, bare := filepath.Join(base, "source"), filepath.Join(base, "acme-lib.git")
	if err = os.MkdirAll(source, 0o755); err != nil {
		return err
	}
	for name, content := range fixture.Files {
		target := filepath.Join(source, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err = os.WriteFile(target, []byte(content), 0o644); err != nil {
			return err
		}
	}
	runGit := func(dir string, args ...string) (string, error) {
		command := exec.Command("git", args...)
		command.Dir = dir
		command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(work, ".empty-gitconfig"))
		output, runErr := command.CombinedOutput()
		if runErr != nil {
			return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), runErr, output)
		}
		return strings.TrimSpace(string(output)), nil
	}
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Acme Fixture"}, {"config", "user.email", "fixture@example.invalid"}, {"add", "."}, {"commit", "-m", "fixture"}} {
		if _, err = runGit(source, args...); err != nil {
			return err
		}
	}
	commit, err := runGit(source, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if _, err = runGit(base, "clone", "--bare", source, bare); err != nil {
		return err
	}
	if fixture.InitWorktree {
		if _, err = runGit(work, "init", "-b", "main"); err != nil {
			return err
		}
	}
	fixtureURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(bare)}).String()
	replacements["{{GIT_URL}}"] = fixtureURL
	replacements["{{GIT_COMMIT}}"] = commit
	return nil
}

func repositoryRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err = os.Stat(filepath.Join(dir, "conformance", "VERSION")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("conformance: repository root not found")
		}
		dir = parent
	}
}

func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
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
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		data, err := os.ReadFile(path)
		if err != nil || bytes.IndexByte(data, 0) >= 0 {
			return err
		}
		updated := replaceAll(string(data), replacements)
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

func readCommands(path string, replacements map[string]string) ([][]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var commands [][]string
	if err = json.Unmarshal(data, &commands); err != nil {
		return nil, fmt.Errorf("command: expected a JSON array of argv arrays: %w", err)
	}
	if len(commands) == 0 {
		return nil, errors.New("command: at least one invocation is required")
	}
	for i := range commands {
		if len(commands[i]) == 0 {
			return nil, fmt.Errorf("command: invocation %d has empty argv", i+1)
		}
		for j := range commands[i] {
			commands[i][j] = replaceAll(commands[i][j], replacements)
		}
	}
	return commands, nil
}

func replaceAll(value string, replacements map[string]string) string {
	for old, replacement := range replacements {
		value = strings.ReplaceAll(value, old, replacement)
	}
	return value
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
		matched, matchErr := regexp.MatchString(line, output)
		if matchErr != nil {
			return fmt.Errorf("line %d: %w", number+1, matchErr)
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
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	for key, value := range result {
		result[key] = replaceAll(value, replacements)
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

func compareExpectedFiles(expectedRoot, work string) error {
	if _, err := os.Stat(expectedRoot); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(expectedRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
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

func matchExpectedFilePatterns(path, root string, replacements map[string]string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var expected map[string][]string
	if err = json.Unmarshal(data, &expected); err != nil {
		return fmt.Errorf("file-patterns.json: %w", err)
	}
	for name, patterns := range expected {
		body, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if readErr != nil {
			return fmt.Errorf("expected file %s: %w", name, readErr)
		}
		for _, pattern := range patterns {
			for token, replacement := range replacements {
				pattern = strings.ReplaceAll(pattern, token, regexp.QuoteMeta(replacement))
			}
			matched, matchErr := regexp.MatchString(pattern, string(body))
			if matchErr != nil {
				return fmt.Errorf("expected file %s pattern %q: %w", name, pattern, matchErr)
			}
			if !matched {
				return fmt.Errorf("expected file %s does not match %q; got %q", name, pattern, string(body))
			}
		}
	}
	return nil
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
		if _, statErr := os.Lstat(filepath.Join(work, filepath.FromSlash(line))); !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("result %s must be absent", line)
		}
	}
	return nil
}
