package npm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/adapter"
)

func TestWireGoldenIdempotentUnwire(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join("..", "..", "testdata", "consumer-npm")
	original := copyFile(t, filepath.Join(fixture, "package.json"), filepath.Join(root, "package.json"))
	dep := adapter.Dependency{Git: "https://github.com/acme/lib-utils.git", Ref: "main", Track: "locked"}
	exp := adapter.Export{Ecosystem: "npm", Name: "@acme/lib-utils"}
	locked := adapter.Locked{Commit: strings.Repeat("a", 40)}
	a := Adapter{}
	change, err := a.Wire(context.Background(), root, dep, exp, locked)
	if err != nil {
		t.Fatal(err)
	}
	if !change.Changed {
		t.Fatal("first wire did not change")
	}
	got, _ := os.ReadFile(filepath.Join(root, "package.json"))
	golden, _ := os.ReadFile(filepath.Join(fixture, "package.golden.json"))
	assertJSONEqual(t, got, golden)
	change, err = a.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || change.Changed {
		t.Fatalf("second wire: %#v %v", change, err)
	}
	change, err = a.Unwire(context.Background(), root, dep, exp)
	if err != nil || !change.Changed {
		t.Fatalf("unwire: %#v %v", change, err)
	}
	var gotDoc, wantDoc any
	_ = json.Unmarshal(mustRead(t, filepath.Join(root, "package.json")), &gotDoc)
	_ = json.Unmarshal(original, &wantDoc)
	if !deepEqual(gotDoc, wantDoc) {
		t.Fatal("unwire did not restore dependency data")
	}
}
func TestDetectVariants(t *testing.T) {
	for _, tc := range []struct{ name, want string }{{"consumer-yarn", "yarn-berry"}, {"consumer-pnpm", "pnpm"}, {"consumer-npm", "npm"}} {
		ok, got, err := (Adapter{}).Detect(filepath.Join("..", "..", "testdata", tc.name))
		if err != nil || !ok || string(got) != tc.want {
			t.Errorf("%s: %v %q %v", tc.name, ok, got, err)
		}
	}
}
func copyFile(t *testing.T, src, dst string) []byte {
	t.Helper()
	b := mustRead(t, src)
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return b
}
func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func assertJSONEqual(t *testing.T, a, b []byte) {
	t.Helper()
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil || !deepEqual(x, y) {
		t.Fatalf("json differs\ngot %s\nwant %s", a, b)
	}
}
func deepEqual(a, b any) bool { return fmtJSON(a) == fmtJSON(b) }
func fmtJSON(v any) string    { b, _ := json.Marshal(v); return string(b) }
