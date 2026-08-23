package gradle

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/adapter"
	"github.com/neprel/git-a2a/internal/manifest"
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
			dep := adapter.Dependency{ID: "acme-lib", Vendor: &manifest.Vendor{Mode: "submodule"}}
			exp := adapter.Export{Ecosystem: "maven", Name: "com.acme:lib-utils", Path: "jvm"}
			locked := adapter.Locked{Path: "modules/lib", Vendor: &manifest.LockedVendor{Mode: "submodule", Path: "deps/acme-lib"}}
			change, err := implementation.Wire(context.Background(), root, dep, exp, locked)
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
			if change, err = implementation.Wire(context.Background(), root, dep, exp, locked); err != nil || change.Changed {
				t.Fatalf("second Wire = %#v, %v", change, err)
			}
			if findings, driftErr := implementation.Drift(context.Background(), root, dep, exp, locked); driftErr != nil || len(findings) != 0 {
				t.Fatalf("Drift = %#v, %v", findings, driftErr)
			}
			if change, err = implementation.Unwire(context.Background(), root, dep, exp); err != nil || !change.Changed {
				t.Fatalf("Unwire = %#v, %v", change, err)
			}
			if restored, readErr := os.ReadFile(filepath.Join(root, settings)); readErr != nil || !bytes.Equal(restored, original) {
				t.Fatalf("settings not restored = %q, %v", restored, readErr)
			}
		})
	}
}

func TestRequiresVendorSortsAndRepairsOwnedGeneratedFile(t *testing.T) {
	root := t.TempDir()
	settings := []byte("rootProject.name = \"consumer\"\n")
	if err := os.WriteFile(filepath.Join(root, "settings.gradle.kts"), settings, 0o644); err != nil {
		t.Fatal(err)
	}
	implementation := Adapter{}
	exp := adapter.Export{Ecosystem: "maven", Name: "com.acme:lib-utils"}
	if _, err := implementation.Wire(context.Background(), root, adapter.Dependency{ID: "acme-z"}, exp, adapter.Locked{}); !adapter.IsNotWirable(err) {
		t.Fatalf("missing vendor error = %v", err)
	}
	for _, id := range []string{"acme-z", "acme-a"} {
		dep := adapter.Dependency{ID: id, Vendor: &manifest.Vendor{Mode: "copy"}}
		locked := adapter.Locked{Vendor: &manifest.LockedVendor{Mode: "copy", Path: "deps/" + id}}
		if _, err := implementation.Wire(context.Background(), root, dep, exp, locked); err != nil {
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
	dep := adapter.Dependency{ID: "acme-a", Vendor: &manifest.Vendor{Mode: "copy"}}
	locked := adapter.Locked{Vendor: &manifest.LockedVendor{Mode: "copy", Path: "deps/acme-a"}}
	change, err := implementation.Wire(context.Background(), root, dep, exp, locked)
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
		dep = adapter.Dependency{ID: id, Vendor: &manifest.Vendor{Mode: "copy"}}
		if change, err = implementation.Unwire(context.Background(), root, dep, exp); err != nil || !change.Changed {
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

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
