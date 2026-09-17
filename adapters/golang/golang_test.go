package golang

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/adapter"
)

func TestWireGoldenIdempotentUnwire(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join("..", "..", "testdata", "consumer-go")
	original, err := os.ReadFile(filepath.Join(fixture, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "go.mod"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{Name: "acme-lib", Git: "https://github.com/acme/lib-utils.git", Ref: "main"}
	exp := adapter.Export{Adapter: "golang", Name: "acme.dev/lib-utils"}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	a := testAdapter()
	change, err := a.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed {
		t.Fatalf("wire: %#v %v", change, err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "go.mod"))
	want, _ := os.ReadFile(filepath.Join(fixture, "go.golden.mod"))
	if string(got) != string(want) {
		t.Fatalf("golden differs\ngot:\n%s\nwant:\n%s", got, want)
	}
	change, err = a.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || change.Changed {
		t.Fatalf("second wire: %#v %v", change, err)
	}
	if findings, err := a.Drift(context.Background(), root, dep, exp, locked); err != nil || len(findings) != 0 {
		t.Fatalf("clean drift: %v %v", findings, err)
	}
	wrong := locked
	wrong.Git = "https://github.com/acme/fork.git"
	if findings, err := a.Drift(context.Background(), root, dep, exp, wrong); err != nil || len(findings) != 1 {
		t.Fatalf("source drift: %v %v", findings, err)
	}
	change, err = a.Unwire(context.Background(), root, dep, exp)
	if err != nil || !change.Changed {
		t.Fatalf("unwire: %#v %v", change, err)
	}
	got, _ = os.ReadFile(filepath.Join(root, "go.mod"))
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(original)) {
		t.Fatalf("unwire differs\ngot:\n%s\nwant:\n%s", got, original)
	}
}

func TestDriftMissingEntryIsUnwired(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module acme.dev/consumer\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err := (Adapter{}).Drift(context.Background(), root,
		adapter.Dependency{Git: "https://github.com/acme/lib.git"},
		adapter.Export{Adapter: "golang", Name: "acme.dev/lib"},
		adapter.Locked{Git: "https://github.com/acme/lib.git", Commit: strings.Repeat("a", 40)})
	if err != nil || len(findings) != 1 {
		t.Fatalf("findings=%v err=%v", findings, err)
	}
}

func TestWireUsesLockedPseudoVersionAndDoesNotDuplicateRequireBlock(t *testing.T) {
	root := t.TempDir()
	original := "module acme.dev/consumer\n\ngo 1.24\n\nrequire (\n\tacme.dev/lib v0.0.0\n\tacme.dev/other v1.2.3\n)\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("b", 40)
	dep := adapter.Dependency{Name: "lib", Git: "https://github.com/acme/lib.git", Ref: "main"}
	exp := adapter.Export{Adapter: "golang", Name: "acme.dev/lib"}
	if _, err := testAdapter().Wire(context.Background(), root, dep, exp, adapter.Locked{Git: dep.Git, Commit: commit}); err != nil {
		t.Fatal(err)
	}
	got := string(mustReadFile(t, filepath.Join(root, "go.mod")))
	if strings.Count(got, "acme.dev/lib v0.0.0") != 1 {
		t.Fatalf("require duplicated:\n%s", got)
	}
	if !strings.Contains(got, "github.com/acme/lib v0.0.0-20260822112233-bbbbbbbbbbbb") || strings.Contains(got, " main") {
		t.Fatalf("dependency was not pinned to locked commit:\n%s", got)
	}
	if findings, err := (Adapter{}).Drift(context.Background(), root, dep, exp, adapter.Locked{Git: dep.Git, Commit: commit}); err != nil || len(findings) != 0 {
		t.Fatalf("drift=%v err=%v", findings, err)
	}
}

func TestWireRejectsMalformedLockedCommitWithoutPanic(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module acme.dev/consumer\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := testAdapter().Wire(context.Background(), root,
		adapter.Dependency{Git: "https://github.com/acme/lib.git"},
		adapter.Export{Adapter: "golang", Name: "acme.dev/lib"},
		adapter.Locked{Commit: "short"})
	if err == nil || !strings.Contains(err.Error(), "40-character") {
		t.Fatalf("err=%v", err)
	}
}

func TestUnwirePreservesPrecedingBlankLines(t *testing.T) {
	root := t.TempDir()
	original := "module acme.dev/consumer\n\ngo 1.24\n\nrequire acme.dev/lib v0.0.0\n\nreplace acme.dev/lib => github.com/acme/lib v0.0.0-00010101000000-aaaaaaaaaaaa\n\nexclude acme.dev/other v1.0.0\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Adapter{}).Unwire(context.Background(), root, adapter.Dependency{}, adapter.Export{Name: "acme.dev/lib"}); err != nil {
		t.Fatal(err)
	}
	got := string(mustReadFile(t, filepath.Join(root, "go.mod")))
	if !strings.Contains(got, "go 1.24\n\nexclude acme.dev/other v1.0.0") {
		t.Fatalf("surrounding blank line was not preserved:\n%s", got)
	}
}

func TestCapabilityRejectsUnrepresentableSourceWithoutMutation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "go.mod")
	original := []byte("module acme.dev/consumer\n\ngo 1.24\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dep := range []adapter.Dependency{
		{Git: "file:///tmp/acme-lib"},
		{Git: "git://127.0.0.1:9418/acme-lib.git"},
		{Git: "https://github.com/acme/lib.git"},
	} {
		exp := adapter.Export{Name: "acme.dev/lib"}
		if strings.HasPrefix(dep.Git, "https:") {
			exp.Path = "../escape"
		}
		err := (Adapter{}).Capability(root, dep, exp)
		if err == nil || !adapter.IsNotWirable(err) {
			t.Fatalf("Capability(%q) error=%v, want typed NotWirable", dep.Git, err)
		}
	}
	if got := mustReadFile(t, path); string(got) != string(original) {
		t.Fatalf("Capability mutated go.mod:\n%s", got)
	}
}

func TestPullAlwaysDownloadsAndInspectReportsMissingMaterialization(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module acme.dev/consumer\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{Git: "https://github.com/acme/lib.git"}
	exp := adapter.Export{Name: "acme.dev/lib"}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("c", 40)}
	downloads := 0
	a := testAdapter()
	a.Download = func(_ context.Context, gotRoot, module string) error {
		downloads++
		if gotRoot != root || module != exp.Name {
			t.Fatalf("Download(%q, %q)", gotRoot, module)
		}
		return nil
	}
	a.Materialized = func(context.Context, string, string) (bool, string, error) {
		return false, "local module directory is missing", nil
	}
	if change, err := a.Pull(context.Background(), root, dep, exp, locked); err != nil || !change.Changed {
		t.Fatalf("first Pull=%#v err=%v", change, err)
	}
	if change, err := a.Pull(context.Background(), root, dep, exp, locked); err != nil || change.Changed {
		t.Fatalf("second Pull=%#v err=%v", change, err)
	}
	if downloads != 2 {
		t.Fatalf("downloads=%d, want one per Pull", downloads)
	}
	findings, err := a.Inspect(context.Background(), root, dep, exp, locked)
	if err != nil || len(findings) != 1 || findings[0].Want != "downloaded module" {
		t.Fatalf("Inspect=%v err=%v", findings, err)
	}
}

func TestRemovePreservesUnrelatedRequirementsAndChecksums(t *testing.T) {
	root := t.TempDir()
	goMod := "module acme.dev/consumer\n\ngo 1.24\n\nrequire (\n\tacme.dev/lib v0.0.0\n\tacme.dev/other v1.2.3\n)\n\nreplace acme.dev/lib => github.com/acme/lib v0.0.0-20260822112233-aaaaaaaaaaaa\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	goSum := []byte("github.com/acme/lib v0.0.0-20260822112233-aaaaaaaaaaaa h1:owned\nacme.dev/other v1.2.3 h1:unrelated\n")
	if err := os.WriteFile(filepath.Join(root, "go.sum"), goSum, 0o644); err != nil {
		t.Fatal(err)
	}
	change, err := (Adapter{}).Remove(context.Background(), root, adapter.Dependency{}, adapter.Export{Name: "acme.dev/lib"}, adapter.Locked{})
	if err != nil || !change.Changed {
		t.Fatalf("Remove=%#v err=%v", change, err)
	}
	got := string(mustReadFile(t, filepath.Join(root, "go.mod")))
	if strings.Contains(got, "acme.dev/lib") || !strings.Contains(got, "acme.dev/other v1.2.3") {
		t.Fatalf("unexpected go.mod after Remove:\n%s", got)
	}
	if gotSum := mustReadFile(t, filepath.Join(root, "go.sum")); string(gotSum) != string(goSum) {
		t.Fatalf("Remove rewrote shared checksum evidence:\n%s", gotSum)
	}
}

func TestRealGoToolPullMaterializesUsableModule(t *testing.T) {
	if os.Getenv("GITA2A_IT_GO") != "1" {
		t.Skip("set GITA2A_IT_GO=1 to exercise the installed Go tool and network")
	}
	root := t.TempDir()
	moduleCache := filepath.Join(root, ".gomodcache")
	t.Setenv("GOMODCACHE", moduleCache)
	t.Cleanup(func() {
		_ = filepath.WalkDir(moduleCache, func(path string, entry os.DirEntry, err error) error {
			if err == nil {
				_ = os.Chmod(path, 0o700)
			}
			return nil
		})
	})
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/consumer\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	remote := "https://github.com/google/uuid.git"
	out, err := exec.Command("git", "ls-remote", remote, "HEAD").Output()
	if err != nil {
		t.Fatalf("resolve integration commit: %v", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 || len(fields[0]) != 40 {
		t.Fatalf("unexpected ls-remote output: %q", out)
	}
	dep := adapter.Dependency{Git: remote}
	exp := adapter.Export{Name: "github.com/google/uuid"}
	locked := adapter.Locked{Git: remote, Commit: fields[0]}
	a := Adapter{}
	if change, err := a.Pull(context.Background(), root, dep, exp, locked); err != nil || !change.Changed {
		t.Fatalf("Pull=%#v err=%v", change, err)
	}
	if findings, err := a.Inspect(context.Background(), root, dep, exp, locked); err != nil || len(findings) != 0 {
		t.Fatalf("Inspect=%v err=%v", findings, err)
	}
	program := []byte("package consumer\n\nimport \"github.com/google/uuid\"\n\nvar _ = uuid.Nil\n")
	if err := os.WriteFile(filepath.Join(root, "consumer_test.go"), program, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-mod=readonly", "./...")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("use downloaded module: %v: %s", err, output)
	}
	if err := os.Remove(filepath.Join(root, "consumer_test.go")); err != nil {
		t.Fatal(err)
	}
	if change, err := a.Remove(context.Background(), root, dep, exp, locked); err != nil || !change.Changed {
		t.Fatalf("Remove=%#v err=%v", change, err)
	}
	if got := string(mustReadFile(t, filepath.Join(root, "go.mod"))); strings.Contains(got, exp.Name) {
		t.Fatalf("Remove left root declaration:\n%s", got)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func testAdapter() Adapter {
	return Adapter{ResolveVersion: func(_ context.Context, _, _, commit string) (string, error) {
		return "v0.0.0-20260822112233-" + commit[:12], nil
	}}
}
