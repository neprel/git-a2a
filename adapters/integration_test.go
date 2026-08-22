package adapters_test

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/neprel/git-a2a/adapters/cargo"
	"github.com/neprel/git-a2a/adapters/clojure"
	"github.com/neprel/git-a2a/adapters/composer"
	"github.com/neprel/git-a2a/adapters/gem"
	"github.com/neprel/git-a2a/adapters/golang"
	"github.com/neprel/git-a2a/adapters/hackage"
	"github.com/neprel/git-a2a/adapters/hex"
	"github.com/neprel/git-a2a/adapters/nix"
	"github.com/neprel/git-a2a/adapters/npm"
	pubadapter "github.com/neprel/git-a2a/adapters/pub"
	"github.com/neprel/git-a2a/adapters/pypi"
	"github.com/neprel/git-a2a/adapters/swift"
	"github.com/neprel/git-a2a/adapters/zig"
	"github.com/neprel/git-a2a/internal/adapter"
)

func TestRealToolchainAdapterLifecycle(t *testing.T) {
	if os.Getenv("GITA2A_IT") != "1" {
		t.Skip("set GITA2A_IT=1 to exercise installed ecosystem tools")
	}
	library, libraryPath, commit := integrationLibrary(t)
	dep := adapter.Dependency{Git: library, Ref: "main", Track: "locked"}
	locked := adapter.Locked{Git: dep.Git, Ref: "refs/heads/main", Commit: commit}
	filter := os.Getenv("GITA2A_IT_ECOSYSTEM")
	cases := []struct {
		name, fixture  string
		implementation adapter.Adapter
		export         adapter.Export
		prepare        func(string) error
	}{
		{"npm", "consumer-npm", npm.Adapter{}, adapter.Export{Ecosystem: "npm", Name: "@acme/lib-utils"}, nil},
		{"yarn", "consumer-yarn", npm.Adapter{}, adapter.Export{Ecosystem: "npm", Name: "@acme/lib-utils"}, nil},
		{"pnpm", "consumer-pnpm", npm.Adapter{}, adapter.Export{Ecosystem: "npm", Name: "@acme/lib-utils"}, nil},
		{"bun", "consumer-npm", npm.Adapter{}, adapter.Export{Ecosystem: "npm", Name: "@acme/lib-utils"}, func(root string) error { return os.WriteFile(filepath.Join(root, "bun.lock"), nil, 0o644) }},
		{"pypi", "consumer-uv", pypi.Adapter{}, adapter.Export{Ecosystem: "pypi", Name: "acme-lib-utils"}, nil},
		{"golang", "consumer-go", golang.Adapter{}, adapter.Export{Ecosystem: "golang", Name: "acme.dev/lib-utils"}, nil},
		{"cargo", "consumer-cargo", cargo.Adapter{}, adapter.Export{Ecosystem: "cargo", Name: "acme-lib-utils"}, nil},
		{"swift", "consumer-swift", swift.Adapter{}, adapter.Export{Ecosystem: "swift", Name: "AcmeLibUtils"}, nil},
		{"pub", "consumer-pub", pubadapter.Adapter{}, adapter.Export{Ecosystem: "pub", Name: "acme_lib_utils"}, nil},
		{"gem", "consumer-gem", gem.Adapter{}, adapter.Export{Ecosystem: "gem", Name: "acme-lib-utils"}, nil},
		{"composer", "consumer-composer", composer.Adapter{}, adapter.Export{Ecosystem: "composer", Name: "acme/lib-utils"}, nil},
		{"hex", "consumer-hex", hex.Adapter{}, adapter.Export{Ecosystem: "hex", Name: "acme_lib_utils"}, nil},
		{"cabal", "consumer-cabal", hackage.Adapter{}, adapter.Export{Ecosystem: "hackage", Name: "acme-lib-utils"}, nil},
		{"stack", "consumer-stack", hackage.Adapter{}, adapter.Export{Ecosystem: "hackage", Name: "acme-lib-utils"}, nil},
		{"zig", "consumer-zig", zig.Adapter{}, adapter.Export{Ecosystem: "zig", Name: "acme_lib_utils", Extensions: map[string]any{"x-zig-hash": "1220" + strings.Repeat("b", 64)}}, nil},
		{"clojure", "consumer-clojure", clojure.Adapter{}, adapter.Export{Ecosystem: "clojure", Name: "acme/lib-utils"}, nil},
		{"nix", "consumer-nix", nix.Adapter{}, adapter.Export{Ecosystem: "nix", Name: "acme-lib-utils"}, nil},
	}
	for _, test := range cases {
		if filter != "" && filter != test.name {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			caseDep, caseLocked := dep, locked
			caseExport := test.export
			if test.name == "pypi" {
				caseDep.Git = "file://" + filepath.ToSlash(libraryPath)
				caseLocked.Git = caseDep.Git
			} else if test.name == "golang" {
				caseDep.Git = "https://github.com/stretchr/testify.git"
				caseLocked.Git = caseDep.Git
				caseLocked.Commit = remoteCommit(t, caseDep.Git)
				caseExport.Name = "github.com/stretchr/testify"
			}
			root := t.TempDir()
			copyTree(t, filepath.Join("..", "testdata", test.fixture), root)
			if test.prepare != nil {
				if err := test.prepare(root); err != nil {
					t.Fatal(err)
				}
			}
			change, err := test.implementation.Wire(context.Background(), root, caseDep, caseExport, caseLocked)
			if err != nil || !change.Changed {
				t.Fatalf("Wire: change=%#v err=%v", change, err)
			}
			if err = test.implementation.Refresh(context.Background(), root, caseDep, caseExport, caseLocked); err != nil {
				t.Fatalf("Refresh: %v", err)
			}
			findings, err := test.implementation.Drift(context.Background(), root, caseDep, caseExport, caseLocked)
			if err != nil || len(findings) != 0 {
				t.Fatalf("Drift: %v err=%v", findings, err)
			}
			change, err = test.implementation.Unwire(context.Background(), root, caseDep, caseExport)
			if err != nil || !change.Changed {
				t.Fatalf("Unwire: change=%#v err=%v", change, err)
			}
		})
	}
}

func remoteCommit(t *testing.T, url string) string {
	t.Helper()
	output := runGit(t, "", "ls-remote", url, "HEAD")
	fields := strings.Fields(output)
	if len(fields) < 1 {
		t.Fatalf("no HEAD at %s", url)
	}
	return fields[0]
}

func integrationLibrary(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"package.json":   `{"name":"@acme/lib-utils","version":"1.0.0"}` + "\n",
		"pyproject.toml": "[project]\nname = \"acme-lib-utils\"\nversion = \"1.0.0\"\n",
		"go.mod":         "module acme.dev/lib-utils\n\ngo 1.24\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.email", "integration@example.test")
	runGit(t, root, "config", "user.name", "Integration")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "fixture")
	commit := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	command := exec.Command("git", "daemon", "--reuseaddr", "--export-all", "--listen=127.0.0.1", fmt.Sprintf("--port=%d", port), "--base-path="+filepath.Dir(root), filepath.Dir(root))
	if err = command.Start(); err != nil {
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
	return "git://" + address + "/" + filepath.Base(root), root, commit
}

func copyTree(t *testing.T, source, destination string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil || rel == "." {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, body, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
}
