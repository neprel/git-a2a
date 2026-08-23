package maven

import (
	"bytes"
	"context"
	"github.com/neprel/git-a2a/internal/adapter"
	"github.com/neprel/git-a2a/internal/manifest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoldenRepairUnwire(t *testing.T) {
	r := t.TempDir()
	orig := []byte("<project>\n  <modules>\n    <module>app</module>\n  </modules>\n</project>\n")
	mustWrite(t, filepath.Join(r, "pom.xml"), orig)
	a := Adapter{}
	d := adapter.Dependency{ID: "acme-lib", Vendor: &manifest.Vendor{Mode: "copy"}}
	e := adapter.Export{Ecosystem: "maven", Name: "com.acme:lib-utils", Path: "java/pom.xml"}
	l := adapter.Locked{Vendor: &manifest.LockedVendor{Mode: "copy", Path: "deps/acme-lib"}}
	c, err := a.Wire(context.Background(), r, d, e, l)
	if err != nil || !c.Changed {
		t.Fatalf("wire=%#v %v", c, err)
	}
	if c, err = a.Wire(context.Background(), r, d, e, l); err != nil || c.Changed {
		t.Fatalf("second=%#v %v", c, err)
	}
	if f, er := a.Drift(context.Background(), r, d, e, l); er != nil || len(f) > 0 {
		t.Fatalf("drift=%v %v", f, er)
	}
	p := filepath.Join(r, filepath.FromSlash(generatedFile))
	want := mustRead(t, p)
	mustWrite(t, p, append(want, []byte("foreign\n")...))
	c, err = a.Wire(context.Background(), r, d, e, l)
	if err != nil || !strings.Contains(c.Warning, "discarded") {
		t.Fatalf("repair=%#v %v", c, err)
	}
	mustWrite(t, p, append(want, []byte("foreign\n")...))
	c, err = a.Unwire(context.Background(), r, d, e)
	if err != nil || !c.Changed {
		t.Fatalf("unwire=%#v %v", c, err)
	}
	if got := mustRead(t, filepath.Join(r, "pom.xml")); !bytes.Equal(got, orig) {
		t.Fatalf("not restored\n%s", got)
	}
}
func TestRequiresVendorAndCoordinate(t *testing.T) {
	r := t.TempDir()
	mustWrite(t, filepath.Join(r, "pom.xml"), []byte("<project/>"))
	a := Adapter{}
	_, e := a.Wire(context.Background(), r, adapter.Dependency{ID: "acme"}, adapter.Export{Name: "bad"}, adapter.Locked{})
	if !adapter.IsNotWirable(e) {
		t.Fatal(e)
	}
}
func mustWrite(t *testing.T, p string, b []byte) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(p), 0o755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, b, 0o644); e != nil {
		t.Fatal(e)
	}
}
func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
