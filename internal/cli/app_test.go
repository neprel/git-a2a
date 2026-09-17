package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestHelpExposesExactlySixDomainCommands(t *testing.T) {
	var out, err bytes.Buffer
	a := New(&out, &err)
	if code := a.Run([]string{"--help"}); code != 0 {
		t.Fatal(code)
	}
	text := out.String()
	for _, name := range []string{"init", "add", "pull", "remove", "list", "whose"} {
		if !strings.Contains(text, "  "+name+" ") {
			t.Errorf("missing %s", name)
		}
	}
	for _, old := range []string{"help", "update", "wire", "who", "contact", "card", "catalog", "trust", "setup", "mcp", "upgrade", "doctor", "validate", "status"} {
		if strings.Contains(text, "  "+old+" ") {
			t.Errorf("legacy command exposed: %s", old)
		}
	}
}

func TestPullWithoutDependenciesIsSuccessfulAndReadOnly(t *testing.T) {
	root := t.TempDir()
	writeTestManifest(t, root, "schema: 2\ncomponent:\n  id: consumer\n")
	before := snapshotTree(t, root)
	var out, errOut bytes.Buffer
	a := New(&out, &errOut)
	a.Root = root
	if code := a.Run([]string{"pull"}); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if out.String() != "No dependencies.\n" || errOut.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", out.String(), errOut.String())
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("pull changed files: before=%v after=%v", before, after)
	}
}

func TestPullWithoutDependenciesStillRejectsInvalidManifest(t *testing.T) {
	root := t.TempDir()
	writeTestManifest(t, root, "schema: 1\ncomponent:\n  id: consumer\n")
	var out, errOut bytes.Buffer
	a := New(&out, &errOut)
	a.Root = root
	if code := a.Run([]string{"pull"}); code != 1 || !strings.Contains(errOut.String(), "schema 1 is not supported") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func TestPullUnknownDependencyStillFails(t *testing.T) {
	root := t.TempDir()
	writeTestManifest(t, root, testManifestWithDependencies())
	var out, errOut bytes.Buffer
	a := New(&out, &errOut)
	a.Root = root
	if code := a.Run([]string{"pull", "missing"}); code != 1 || !strings.Contains(errOut.String(), `dependency "missing" not found`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func TestListAlwaysReportsAllDependenciesAndRejectsNames(t *testing.T) {
	root := t.TempDir()
	writeTestManifest(t, root, testManifestWithDependencies())
	var out, errOut bytes.Buffer
	a := New(&out, &errOut)
	a.Root = root
	if code := a.Run([]string{"list", "utils"}); code != 2 || !strings.Contains(errOut.String(), "use git a2a whose utils") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := a.Run([]string{"list", "--json"}); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	var items []map[string]any
	if err := json.Unmarshal(out.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0]["name"] != "stable" || items[1]["name"] != "utils" {
		t.Fatalf("items=%v", items)
	}
}

func TestListEmptyTextAndJSON(t *testing.T) {
	root := t.TempDir()
	writeTestManifest(t, root, "schema: 2\ncomponent:\n  id: consumer\n")
	for _, test := range []struct {
		args []string
		want string
	}{{[]string{"list"}, ""}, {[]string{"list", "--json"}, "[]\n"}} {
		var out, errOut bytes.Buffer
		a := New(&out, &errOut)
		a.Root = root
		if code := a.Run(test.args); code != 0 || out.String() != test.want || errOut.Len() != 0 {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", test.args, code, out.String(), errOut.String())
		}
	}
}

func TestWhoseRequiresKnownAlias(t *testing.T) {
	root := t.TempDir()
	writeTestManifest(t, root, testManifestWithDependencies())
	for _, test := range []struct {
		args []string
		code int
		want string
	}{{[]string{"whose"}, 2, "exactly one dependency name is required"}, {[]string{"whose", "missing"}, 1, `dependency "missing" not found`}} {
		var out, errOut bytes.Buffer
		a := New(&out, &errOut)
		a.Root = root
		if code := a.Run(test.args); code != test.code || !strings.Contains(errOut.String(), test.want) {
			t.Fatalf("args=%v code=%d stderr=%q", test.args, code, errOut.String())
		}
	}
}

func TestWhoseReportsHTTPSCardSurfaceAndJSONObjectOffline(t *testing.T) {
	root := t.TempDir()
	writeOwnershipFixture(t, root, "https://agents.example/utils/card.json", "https://agents.example/utils/card.json", true)
	before := snapshotTree(t, root)
	var out, errOut bytes.Buffer
	a := New(&out, &errOut)
	a.Root = root
	a.Runner = panicRunner{}
	if code := a.Run([]string{"whose", "utils", "--json"}); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	var item map[string]any
	if err := json.Unmarshal(out.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item["name"] != "utils" || item["surface"] != ".git-a2a/surfaces/utils" {
		t.Fatalf("item=%v", item)
	}
	for _, field := range []string{"git", "ref", "commit", "bindings"} {
		if _, ok := item[field]; ok {
			t.Fatalf("whose JSON unexpectedly contains %s: %v", field, item)
		}
	}
	agent := item["agent"].(map[string]any)
	if agent["card"] != "https://agents.example/utils/card.json" {
		t.Fatalf("agent=%v", agent)
	}
	if after := snapshotTree(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("whose changed files: before=%v after=%v", before, after)
	}
}

func TestWhoseReportsLocalCardAndOptionalSurface(t *testing.T) {
	root := t.TempDir()
	writeOwnershipFixture(t, root, "metadata/owner.json", ".git-a2a/agents/utils/agent-card.json", false)
	card := filepath.Join(root, ".git-a2a", "agents", "utils", "agent-card.json")
	if err := os.MkdirAll(filepath.Dir(card), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(card, []byte("opaque card\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	a := New(&out, &errOut)
	a.Root = root
	if code := a.Run([]string{"whose", "utils"}); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "card: .git-a2a/agents/utils/agent-card.json") || strings.Contains(out.String(), "surface:") {
		t.Fatalf("stdout=%q", out.String())
	}
}

func TestWhoseExplainsMissingLocalMetadata(t *testing.T) {
	root := t.TempDir()
	writeOwnershipFixture(t, root, "metadata/owner.json", ".git-a2a/agents/utils/agent-card.json", true)
	if err := os.RemoveAll(filepath.Join(root, ".git-a2a")); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	a := New(&out, &errOut)
	a.Root = root
	if code := a.Run([]string{"whose", "utils"}); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "card: unavailable") || !strings.Contains(out.String(), "surface unavailable") || !strings.Contains(out.String(), "git a2a pull utils") {
		t.Fatalf("stdout=%q", out.String())
	}
}
func TestLegacyCommandsAreUnknown(t *testing.T) {
	for _, name := range []string{"help", "update", "install", "restore", "show", "validate", "status", "who", "contact", "card", "catalog", "trust", "setup", "mcp", "upgrade", "wire", "fetch", "sync", "doctor", "pin", "unpin"} {
		var out, err bytes.Buffer
		a := New(&out, &err)
		if code := a.Run([]string{name}); code != 2 || !strings.Contains(err.String(), "unknown command") {
			t.Errorf("%s: code=%d err=%q", name, code, err.String())
		}
	}
}
func TestInitCreatesIncompleteSchema2(t *testing.T) {
	root := t.TempDir()
	var out, err bytes.Buffer
	a := New(&out, &err)
	a.Root = root
	if code := a.Run([]string{"init", "--id", "consumer"}); code != 0 {
		t.Fatalf("code=%d err=%s", code, err.String())
	}
	if !strings.Contains(err.String(), "agent.card") {
		t.Fatalf("missing publication warning: %s", err.String())
	}
}

type panicRunner struct{}

func (panicRunner) Run(context.Context, string, []byte, ...string) ([]byte, error) {
	panic("whose attempted a Git/network operation")
}

func writeTestManifest(t *testing.T, root, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "a2amodule.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testManifestWithDependencies() string {
	return "schema: 2\ncomponent:\n  id: consumer\ndependencies:\n  - name: utils\n    git: https://example.invalid/utils.git\n    ref: main\n  - name: stable\n    git: https://example.invalid/stable.git\n    ref: main\n"
}

func writeOwnershipFixture(t *testing.T, root, declaredCard, card string, surface bool) {
	t.Helper()
	writeTestManifest(t, root, "schema: 2\ncomponent:\n  id: consumer\ndependencies:\n  - name: utils\n    git: https://example.invalid/utils.git\n    ref: main\n")
	surfaceLine := ""
	if surface {
		surfaceLine = "    surface: .git-a2a/surfaces/utils\n"
		dir := filepath.Join(root, ".git-a2a", "surfaces", "utils")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	lock := fmt.Sprintf("schema: 2\ndependencies:\n  utils:\n    component: acme-utils\n    git: https://example.invalid/utils.git\n    ref: main\n    commit: 0123456789abcdef0123456789abcdef01234567\n    manifest: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n    bindings: []\n    agent:\n      name: utils-agent\n      declaredCard: %s\n      card: %s\n      commit: 0123456789abcdef0123456789abcdef01234567\n%s", declaredCard, card, surfaceLine)
	if err := os.WriteFile(filepath.Join(root, "a2amodule.lock"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
}

func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		if entry.IsDir() {
			out[rel] = "dir"
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(body)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
