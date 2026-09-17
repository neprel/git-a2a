package hackage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/v2/internal/adapter"
)

func TestWireGoldenIdempotentUnwire(t *testing.T) {
	for _, tc := range []struct {
		fixture, file, golden string
	}{
		{"consumer-cabal", "cabal.project", "cabal.golden.project"},
		{"consumer-stack", "stack.yaml", "stack.golden.yaml"},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			root := t.TempDir()
			fixture := filepath.Join("..", "..", "testdata", tc.fixture)
			original := mustRead(t, filepath.Join(fixture, tc.file))
			if err := os.WriteFile(filepath.Join(root, tc.file), original, 0o644); err != nil {
				t.Fatal(err)
			}
			dep := adapter.Dependency{Name: "acme-lib-utils", Git: "https://github.com/acme/lib-utils.git", Ref: "main"}
			exp := adapter.Export{Adapter: "hackage", Name: "acme-lib-utils", Path: "haskell/lib"}
			locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
			a := Adapter{}
			change, err := a.Wire(context.Background(), root, dep, exp, locked)
			if err != nil || !change.Changed {
				t.Fatalf("wire=%#v err=%v", change, err)
			}
			if got, want := mustRead(t, filepath.Join(root, tc.file)), mustRead(t, filepath.Join(fixture, tc.golden)); string(got) != string(want) {
				t.Fatalf("golden differs\ngot:\n%s\nwant:\n%s", got, want)
			}
			if change, err = a.Wire(context.Background(), root, dep, exp, locked); err != nil || change.Changed {
				t.Fatalf("second wire=%#v err=%v", change, err)
			}
			if findings, err := a.Drift(context.Background(), root, dep, exp, locked); err != nil || len(findings) != 0 {
				t.Fatalf("drift=%v err=%v", findings, err)
			}
			if change, err = a.Unwire(context.Background(), root, dep, exp); err != nil || !change.Changed {
				t.Fatalf("unwire=%#v err=%v", change, err)
			}
			if got := mustRead(t, filepath.Join(root, tc.file)); string(got) != string(original) {
				t.Fatalf("unwire differs\ngot:\n%s\nwant:\n%s", got, original)
			}
		})
	}
}

func TestPullChecksSavedToolBeforeMutation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cabal.project")
	original := []byte("packages: ./consumer\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	dep := adapter.Dependency{Git: "https://example.com/acme/lib.git"}
	exp := adapter.Export{Name: "acme-lib"}
	_, err := (Adapter{}).Pull(context.Background(), root, dep, exp, adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)})
	if !adapter.IsMissingTool(err) {
		t.Fatalf("err=%v, want missing-tool", err)
	}
	if got := mustRead(t, path); string(got) != string(original) {
		t.Fatal("Pull mutated cabal.project before tool preflight")
	}
}

func TestStackCreatedHeaderTransfersAcrossManagedDependencies(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "stack.yaml")
	original := []byte("resolver: lts-23.24\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	a := Adapter{}
	locked := adapter.Locked{Commit: strings.Repeat("a", 40)}
	firstDep := adapter.Dependency{Git: "https://example.com/first.git"}
	secondDep := adapter.Dependency{Git: "https://example.com/second.git"}
	first := adapter.Export{Name: "first"}
	second := adapter.Export{Name: "second"}
	if _, err := a.Wire(context.Background(), root, firstDep, first, locked); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Wire(context.Background(), root, secondDep, second, locked); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Unwire(context.Background(), root, firstDep, first); err != nil {
		t.Fatal(err)
	}
	afterFirst := string(mustRead(t, path))
	if !strings.Contains(afterFirst, "extra-deps: # git-a2a:created second\n") || !strings.Contains(afterFirst, "git-a2a:begin second") {
		t.Fatalf("created header was not transferred:\n%s", afterFirst)
	}
	secondLocked := locked
	secondLocked.Git = secondDep.Git
	if findings, err := a.Drift(context.Background(), root, secondDep, second, secondLocked); err != nil || len(findings) != 0 {
		t.Fatalf("remaining dependency drift=%v err=%v", findings, err)
	}
	if _, err := a.Unwire(context.Background(), root, secondDep, second); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, path); string(got) != string(original) {
		t.Fatalf("final removal differs\ngot:\n%s\nwant:\n%s", got, original)
	}
}

func TestLifecycleUsesMaterializingPullAndPlanningRemove(t *testing.T) {
	for _, tc := range []struct {
		name, marker, variant, tool string
		pullArgs, removeArgs        string
	}{
		{name: "cabal", marker: "cabal.project", variant: "cabal", tool: "cabal", pullArgs: "build all", removeArgs: "build all --dry-run"},
		{name: "stack", marker: "stack.yaml", variant: "stack", tool: "stack", pullArgs: "build", removeArgs: "build --dry-run"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			body := "packages: ./consumer\n"
			if tc.variant == "stack" {
				body = "resolver: lts-23.24\n"
			}
			if err := os.WriteFile(filepath.Join(root, tc.marker), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			log := filepath.Join(root, "manager.log")
			script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + log + "\n"
			if err := os.WriteFile(filepath.Join(bin, tc.tool), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			dep := adapter.Dependency{Git: "https://example.com/acme/lib.git"}
			exp := adapter.Export{Name: "acme-lib"}
			locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
			a := Adapter{}
			if _, err := a.Pull(context.Background(), root, dep, exp, locked); err != nil {
				t.Fatal(err)
			}
			if _, err := a.Remove(context.Background(), root, dep, exp, locked); err != nil {
				t.Fatal(err)
			}
			got := string(mustRead(t, log))
			want := "--version\n" + tc.pullArgs + "\n--version\n" + tc.removeArgs + "\n"
			if got != want {
				t.Fatalf("manager commands = %q, want %q", got, want)
			}
		})
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
