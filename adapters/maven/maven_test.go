package maven

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/adapter"
)

func TestGoldenRepairUnwire(t *testing.T) {
	r := t.TempDir()
	orig := []byte("<project>\n  <modules>\n    <module>app</module>\n  </modules>\n</project>\n")
	mustWrite(t, filepath.Join(r, "pom.xml"), orig)
	a := Adapter{}
	d := adapter.Dependency{Name: "acme-lib"}
	e := adapter.Export{Adapter: "maven", Name: "com.acme:lib-utils", Path: "deps/acme-lib/java/pom.xml"}
	l := adapter.Locked{Commit: strings.Repeat("a", 40)}
	c, err := a.wire(context.Background(), r, d, e, l)
	if err != nil || !c.Changed {
		t.Fatalf("wire=%#v %v", c, err)
	}
	if c, err = a.wire(context.Background(), r, d, e, l); err != nil || c.Changed {
		t.Fatalf("second=%#v %v", c, err)
	}
	if f, er := a.inspectDeclaration(context.Background(), r, d, e, l); er != nil || len(f) > 0 {
		t.Fatalf("drift=%v %v", f, er)
	}
	p := filepath.Join(r, filepath.FromSlash(generatedFile))
	want := mustRead(t, p)
	mustWrite(t, p, append(want, []byte("foreign\n")...))
	c, err = a.wire(context.Background(), r, d, e, l)
	if err != nil || !strings.Contains(c.Warning, "discarded") {
		t.Fatalf("repair=%#v %v", c, err)
	}
	mustWrite(t, p, append(want, []byte("foreign\n")...))
	c, err = a.unwire(context.Background(), r, d, e)
	if err != nil || !c.Changed {
		t.Fatalf("unwire=%#v %v", c, err)
	}
	if got := mustRead(t, filepath.Join(r, "pom.xml")); !bytes.Equal(got, orig) {
		t.Fatalf("not restored\n%s", got)
	}
}
func TestRequiresCheckoutPathAndCoordinate(t *testing.T) {
	r := t.TempDir()
	mustWrite(t, filepath.Join(r, "pom.xml"), []byte("<project/>"))
	a := Adapter{}
	original := mustRead(t, filepath.Join(r, "pom.xml"))
	if e := a.Capability(r, adapter.Dependency{Name: "acme"}, adapter.Export{Name: "bad"}); !adapter.IsNotWirable(e) {
		t.Fatalf("Capability error = %v", e)
	}
	if got := mustRead(t, filepath.Join(r, "pom.xml")); !bytes.Equal(got, original) {
		t.Fatal("Capability mutated the consumer")
	}
	_, e := a.wire(context.Background(), r, adapter.Dependency{Name: "acme"}, adapter.Export{Name: "bad"}, adapter.Locked{})
	if !adapter.IsNotWirable(e) {
		t.Fatal(e)
	}
}

func TestInspectReportsMissingModule(t *testing.T) {
	r := t.TempDir()
	mustWrite(t, filepath.Join(r, "pom.xml"), []byte("<project></project>\n"))
	d := adapter.Dependency{Name: "acme"}
	e := adapter.Export{Name: "com.acme:lib", Path: "deps/acme"}
	if _, err := (Adapter{}).wire(context.Background(), r, d, e, adapter.Locked{}); err != nil {
		t.Fatal(err)
	}
	f, err := (Adapter{}).Inspect(context.Background(), r, d, e, adapter.Locked{})
	if err != nil || len(f) != 1 || f[0].Got == "" {
		t.Fatalf("Inspect = %#v, %v", f, err)
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
