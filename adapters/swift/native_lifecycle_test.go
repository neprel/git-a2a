package swift

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPublicCLISwiftLifecycle exercises SwiftPM through the built public CLI.
// The native runner opts in explicitly; after that, missing advertised tools
// are failures rather than skips.
func TestPublicCLISwiftLifecycle(t *testing.T) {
	if os.Getenv("GITA2A_IT_SWIFT") != "1" {
		t.Skip("set GITA2A_IT_SWIFT=1 to exercise the public CLI with SwiftPM")
	}
	for _, tool := range []string{"git", "swift"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("required native tool %s: %v", tool, err)
		}
	}

	bin := buildSwiftCLI(t)
	base := t.TempDir()
	upstream := filepath.Join(base, "native-lib")
	writeSwiftNative(t, filepath.Join(upstream, "a2amodule.yml"), "schema: 2\ncomponent:\n  id: native-lib\n  exports:\n    - adapter: swift\n      name: NativeLib\nagent:\n  card: https://agents.example/native-lib.json\n")
	writeSwiftNative(t, filepath.Join(upstream, "Package.swift"), `// swift-tools-version: 6.0
import PackageDescription
let package = Package(
    name: "NativeLib",
    products: [.library(name: "NativeLib", targets: ["NativeLib"])],
    targets: [.target(name: "NativeLib")]
)
`)
	writeSwiftNative(t, filepath.Join(upstream, "Sources", "NativeLib", "Lib.swift"), "public let nativeValue = \"one\"\n")
	initSwiftGit(t, upstream)
	first := strings.TrimSpace(runSwiftNative(t, upstream, "git", "rev-parse", "HEAD"))

	consumer := filepath.Join(base, "consumer")
	writeSwiftNative(t, filepath.Join(consumer, "Unrelated", "Package.swift"), `// swift-tools-version: 6.0
import PackageDescription
let package = Package(
    name: "Unrelated",
    products: [.library(name: "Unrelated", targets: ["Unrelated"])],
    targets: [.target(name: "Unrelated")]
)
`)
	writeSwiftNative(t, filepath.Join(consumer, "Unrelated", "Sources", "Unrelated", "Lib.swift"), "public let unrelatedValue = \"seven\"\n")
	manifest := `// swift-tools-version: 6.0
import PackageDescription
let package = Package(
    name: "Consumer",
    dependencies: [.package(path: "Unrelated")],
    targets: [.executableTarget(name: "Consumer", dependencies: [
        .product(name: "Unrelated", package: "unrelated"),
        .product(name: "NativeLib", package: "native-lib"),
    ])]
)
`
	writeSwiftNative(t, filepath.Join(consumer, "Package.swift"), manifest)
	writeSwiftNative(t, filepath.Join(consumer, "Sources", "Consumer", "main.swift"), "import NativeLib\nimport Unrelated\nprint(nativeValue + \":\" + unrelatedValue)\n")
	initSwiftGit(t, consumer)

	source := swiftFileURL(upstream)
	runSwiftNative(t, consumer, bin, "init", "--id", "consumer")
	runSwiftNative(t, consumer, bin, "add", source, "--name", "native-lib", "--ref", "main")
	assertSwiftUse(t, consumer, "one:seven")
	assertSwiftLockCommit(t, consumer, first)

	// A clone has no ignored .build checkout. Pull must recreate usable state.
	ignorePath := filepath.Join(consumer, ".gitignore")
	writeSwiftNative(t, ignorePath, string(readSwiftNative(t, ignorePath))+".build/\n")
	runSwiftNative(t, consumer, "git", "add", "-A")
	runSwiftNative(t, consumer, "git", "commit", "-m", "installed")
	fresh := filepath.Join(base, "fresh")
	runSwiftNative(t, base, "git", "clone", consumer, fresh)
	runSwiftNative(t, fresh, bin, "pull", "native-lib")
	assertSwiftUse(t, fresh, "one:seven")

	// Deleting SwiftPM's checkout must be repaired even at an unchanged commit.
	if err := os.RemoveAll(filepath.Join(consumer, ".build", "checkouts")); err != nil {
		t.Fatal(err)
	}
	runSwiftNative(t, consumer, bin, "pull", "native-lib")
	assertSwiftUse(t, consumer, "one:seven")

	// The upstream version metadata is unchanged; a targeted pull still advances
	// the branch to its new exact commit and makes the new code usable.
	writeSwiftNative(t, filepath.Join(upstream, "Sources", "NativeLib", "Lib.swift"), "public let nativeValue = \"two\"\n")
	runSwiftNative(t, upstream, "git", "commit", "-am", "same-version advance")
	next := strings.TrimSpace(runSwiftNative(t, upstream, "git", "rev-parse", "HEAD"))
	runSwiftNative(t, consumer, bin, "pull", "native-lib")
	assertSwiftUse(t, consumer, "two:seven")
	assertSwiftLockCommit(t, consumer, next)

	// Product references are user-owned, so remove that use explicitly before
	// the CLI removes its package expression. The unrelated package must remain.
	withoutUse := strings.ReplaceAll(string(readSwiftNative(t, filepath.Join(consumer, "Package.swift"))), "        .product(name: \"NativeLib\", package: \"native-lib\"),\n", "")
	writeSwiftNative(t, filepath.Join(consumer, "Package.swift"), withoutUse)
	writeSwiftNative(t, filepath.Join(consumer, "Sources", "Consumer", "main.swift"), "import Unrelated\nprint(unrelatedValue)\n")
	runSwiftNative(t, consumer, bin, "remove", "native-lib")
	assertSwiftUse(t, consumer, "seven")
	packageBody := string(readSwiftNative(t, filepath.Join(consumer, "Package.swift")))
	if strings.Contains(packageBody, source) || !strings.Contains(packageBody, `.package(path: "Unrelated")`) {
		t.Fatalf("Swift declarations after remove:\n%s", packageBody)
	}
	resolvedPath := filepath.Join(consumer, "Package.resolved")
	if resolved, err := os.ReadFile(resolvedPath); err == nil {
		if strings.Contains(string(resolved), source) {
			t.Fatalf("removed Swift pin remains:\n%s", resolved)
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func buildSwiftCLI(t *testing.T) string {
	t.Helper()
	if binary := os.Getenv("GITA2A_CLI_BIN"); binary != "" {
		return binary
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatalf("go is required when GITA2A_CLI_BIN is unset: %v", err)
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	binary := filepath.Join(t.TempDir(), "git-a2a")
	runSwiftNative(t, repo, "go", "build", "-o", binary, "./cmd/git-a2a")
	return binary
}

func initSwiftGit(t *testing.T, root string) {
	t.Helper()
	runSwiftNative(t, root, "git", "init", "-b", "main")
	runSwiftNative(t, root, "git", "config", "user.name", "git-a2a native")
	runSwiftNative(t, root, "git", "config", "user.email", "native@example.invalid")
	runSwiftNative(t, root, "git", "add", ".")
	runSwiftNative(t, root, "git", "commit", "-m", "one")
}

func assertSwiftUse(t *testing.T, root, want string) {
	t.Helper()
	got := strings.TrimSpace(runSwiftNative(t, root, "swift", "run", "--disable-sandbox", "Consumer"))
	if !strings.HasSuffix(got, want) {
		t.Fatalf("Swift consumer output=%q, want suffix %q", got, want)
	}
}

func assertSwiftLockCommit(t *testing.T, root, want string) {
	t.Helper()
	body := string(readSwiftNative(t, filepath.Join(root, "a2amodule.lock")))
	if !strings.Contains(body, "commit: "+want) {
		t.Fatalf("a2amodule.lock lacks commit %s:\n%s", want, body)
	}
}

func swiftFileURL(path string) string {
	path = filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		return "file:///" + strings.TrimPrefix(path, "/")
	}
	return "file://" + path
}

func writeSwiftNative(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readSwiftNative(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func runSwiftNative(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_ALLOW_PROTOCOL=file")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}
