package gradle

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/v2/internal/adapter"
)

func TestKotlinAndGroovyGoldenLifecycle(t *testing.T) {
	for _, variant := range []adapter.Variant{"gradle-kts", "gradle-groovy"} {
		t.Run(string(variant), func(t *testing.T) {
			root := t.TempDir()
			generated, settings, line := files(variant)
			fixture := filepath.Join("..", "..", "testdata", "consumer-"+string(variant))
			original := mustRead(t, filepath.Join(fixture, settings))
			if err := os.WriteFile(filepath.Join(root, settings), original, 0o644); err != nil {
				t.Fatal(err)
			}
			implementation := Adapter{}
			dep := adapter.Dependency{Name: "acme-lib"}
			exp := adapter.Export{Adapter: "maven", Name: "com.acme:lib-utils", Path: "deps/acme-lib/modules/lib/jvm"}
			locked := adapter.Locked{Commit: strings.Repeat("a", 40)}
			change, err := implementation.wire(context.Background(), root, dep, exp, locked)
			if err != nil || !change.Changed {
				t.Fatalf("Wire = %#v, %v", change, err)
			}
			settingsBody, _ := os.ReadFile(filepath.Join(root, settings))
			if !hasLine(settingsBody, line) {
				t.Fatalf("managed line missing: %s", settingsBody)
			}
			generatedBody, _ := os.ReadFile(filepath.Join(root, generated))
			goldenName := "generated.golden.gradle.kts"
			if variant == "gradle-groovy" {
				goldenName = "generated.golden.gradle"
			}
			if want := mustRead(t, filepath.Join(fixture, goldenName)); !bytes.Equal(generatedBody, want) {
				t.Fatalf("generated golden differs\ngot:\n%s\nwant:\n%s", generatedBody, want)
			}
			for _, want := range []string{"deps/acme-lib/modules/lib/jvm", "com.acme:lib-utils"} {
				if !strings.Contains(string(generatedBody), want) {
					t.Fatalf("generated missing %q:\n%s", want, generatedBody)
				}
			}
			if change, err = implementation.wire(context.Background(), root, dep, exp, locked); err != nil || change.Changed {
				t.Fatalf("second Wire = %#v, %v", change, err)
			}
			if findings, driftErr := implementation.inspectDeclaration(context.Background(), root, dep, exp, locked); driftErr != nil || len(findings) != 0 {
				t.Fatalf("Drift = %#v, %v", findings, driftErr)
			}
			if change, err = implementation.unwire(context.Background(), root, dep, exp); err != nil || !change.Changed {
				t.Fatalf("Unwire = %#v, %v", change, err)
			}
			if restored, readErr := os.ReadFile(filepath.Join(root, settings)); readErr != nil || !bytes.Equal(restored, original) {
				t.Fatalf("settings not restored = %q, %v", restored, readErr)
			}
		})
	}
}

func TestRequiresCheckoutPathSortsAndRepairsOwnedGeneratedFile(t *testing.T) {
	root := t.TempDir()
	settings := []byte("rootProject.name = \"consumer\"\n")
	if err := os.WriteFile(filepath.Join(root, "settings.gradle.kts"), settings, 0o644); err != nil {
		t.Fatal(err)
	}
	implementation := Adapter{}
	exp := adapter.Export{Adapter: "maven", Name: "com.acme:lib-utils"}
	before := append([]byte(nil), settings...)
	if err := implementation.Capability(root, adapter.Dependency{Name: "acme-z"}, exp); !adapter.IsNotWirable(err) {
		t.Fatalf("Capability error = %v", err)
	}
	if after := mustRead(t, filepath.Join(root, "settings.gradle.kts")); !bytes.Equal(after, before) {
		t.Fatal("Capability mutated the consumer")
	}
	if _, err := implementation.wire(context.Background(), root, adapter.Dependency{Name: "acme-z"}, exp, adapter.Locked{}); !adapter.IsNotWirable(err) {
		t.Fatalf("missing checkout path error = %v", err)
	}
	for _, id := range []string{"acme-z", "acme-a"} {
		dep := adapter.Dependency{Name: id}
		exp.Path = "deps/" + id
		if _, err := implementation.wire(context.Background(), root, dep, exp, adapter.Locked{Commit: strings.Repeat("b", 40)}); err != nil {
			t.Fatal(err)
		}
	}
	generated := filepath.Join(root, "deps", "git-a2a.settings.gradle.kts")
	body := mustRead(t, generated)
	if strings.Index(string(body), "acme-a") > strings.Index(string(body), "acme-z") {
		t.Fatalf("blocks not sorted:\n%s", body)
	}
	if err := os.WriteFile(generated, append(append([]byte(nil), body...), []byte("println(\"foreign\")\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{Name: "acme-a"}
	exp.Path = "deps/acme-a"
	locked := adapter.Locked{Commit: strings.Repeat("b", 40)}
	change, err := implementation.wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed || !strings.Contains(change.Warning, "discarded") {
		t.Fatalf("repair Wire = %#v, %v", change, err)
	}
	if got := mustRead(t, generated); !bytes.Equal(got, body) {
		t.Fatalf("repair changed canonical blocks:\ngot:\n%s\nwant:\n%s", got, body)
	}
	if err := os.WriteFile(generated, append(append([]byte(nil), body...), []byte("println(\"foreign\")\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"acme-a", "acme-z"} {
		dep = adapter.Dependency{Name: id}
		if change, err = implementation.unwire(context.Background(), root, dep, exp); err != nil || !change.Changed {
			t.Fatalf("Unwire %s = %#v, %v", id, change, err)
		}
	}
	if _, statErr := os.Stat(generated); !os.IsNotExist(statErr) {
		t.Fatalf("generated file remains: %v", statErr)
	}
	if restored := mustRead(t, filepath.Join(root, "settings.gradle.kts")); !bytes.Equal(restored, settings) {
		t.Fatalf("settings not restored = %q", restored)
	}
}

func TestInspectReportsMissingCompositeCheckout(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "settings.gradle.kts"), []byte("rootProject.name = \"consumer\"\n"), 0o644)
	dep := adapter.Dependency{Name: "acme"}
	exp := adapter.Export{Adapter: "maven", Name: "com.acme:lib", Path: "deps/acme"}
	if _, err := (Adapter{}).wire(context.Background(), root, dep, exp, adapter.Locked{}); err != nil {
		t.Fatal(err)
	}
	findings, err := (Adapter{}).Inspect(context.Background(), root, dep, exp, adapter.Locked{})
	if err != nil || len(findings) != 1 || findings[0].Got != "missing" {
		t.Fatalf("Inspect = %#v, %v", findings, err)
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
