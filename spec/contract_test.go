package spec_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/manifest"
)

func TestHintCompiles(t *testing.T) {
	command := exec.Command("hint", "spec/_.hint")
	command.Dir = ".."
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("compile spec HINT: %v: %s", err, output)
	}
}

func TestContractFieldsMatchHintSchemasAndGoModel(t *testing.T) {
	hintBytes := read(t, "_.hint")
	hintFields := fieldsFromHint(t, hintBytes)
	manifestSchema := readSchema(t, "schema/a2amodule.schema.json")
	lockSchema := readSchema(t, "schema/a2amodule.lock.schema.json")

	checks := []struct {
		name       string
		schema     map[string]any
		definition string
		goType     reflect.Type
	}{
		{"manifest", manifestSchema, "", reflect.TypeOf(manifest.Manifest{})},
		{"component", manifestSchema, "component", reflect.TypeOf(manifest.Component{})},
		{"export", manifestSchema, "export", reflect.TypeOf(manifest.Export{})},
		{"agent", manifestSchema, "agent", reflect.TypeOf(manifest.Agent{})},
		{"dependency", manifestSchema, "dependency", reflect.TypeOf(manifest.Dependency{})},
		{"binding", manifestSchema, "binding", reflect.TypeOf(manifest.Binding{})},
		{"lock", lockSchema, "", reflect.TypeOf(manifest.Lock{})},
		{"locked_dependency", lockSchema, "lockedDependency", reflect.TypeOf(manifest.LockedDependency{})},
		{"locked_agent", lockSchema, "lockedAgent", reflect.TypeOf(manifest.LockedAgent{})},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			want := hintFields[check.name]
			if len(want) == 0 {
				t.Fatalf("HINT has no fields for %q", check.name)
			}
			assertFields(t, "JSON Schema", want, schemaFields(t, check.schema, check.definition))
			assertFields(t, "Go model", want, goFields(check.goType))
		})
	}
}

func TestSchemasAreVersionTwoAndClosed(t *testing.T) {
	manifestSchema := readSchema(t, "schema/a2amodule.schema.json")
	lockSchema := readSchema(t, "schema/a2amodule.lock.schema.json")
	for name, schema := range map[string]map[string]any{"manifest": manifestSchema, "lock": lockSchema} {
		if got := schema["$id"]; !strings.Contains(got.(string), ".v2.json") {
			t.Errorf("%s schema id = %q, want v2", name, got)
		}
		if got := schema["properties"].(map[string]any)["schema"].(map[string]any)["const"]; got != float64(2) {
			t.Errorf("%s schema const = %v, want 2", name, got)
		}
		if schema["additionalProperties"] != false {
			t.Errorf("%s root must reject unknown fields", name)
		}
	}
	for _, definition := range []string{"component", "export", "agent", "dependency", "binding"} {
		assertClosedDefinition(t, manifestSchema, definition)
	}
	for _, definition := range []string{"lockedAgent", "binding", "lockedDependency"} {
		assertClosedDefinition(t, lockSchema, definition)
	}
}

func TestValidExamplesParseWithReferenceModel(t *testing.T) {
	paths, err := filepath.Glob("examples/*.a2amodule.yml")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 7 {
		t.Fatalf("valid manifest examples = %d, want 7", len(paths))
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if _, err := manifest.Parse(read(t, path)); err != nil {
				t.Fatalf("parse schema 2 example: %v", err)
			}
		})
	}
	locks, err := filepath.Glob("examples/*.a2amodule.lock")
	if err != nil {
		t.Fatal(err)
	}
	if len(locks) == 0 {
		t.Fatal("no lock example")
	}
	for _, path := range locks {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if _, err := manifest.LoadLock(path); err != nil {
				t.Fatalf("parse schema 2 lock example: %v", err)
			}
		})
	}
}

func TestExamplesCoverRequiredScenarios(t *testing.T) {
	native := parseExample(t, "native.a2amodule.yml")
	assertBinding(t, native, "logging", "golang", "go")

	polyglot := parseExample(t, "polyglot.a2amodule.yml")
	if got := len(polyglot.Dependencies[0].Bindings); got != 3 {
		t.Fatalf("polyglot bindings = %d, want 3", got)
	}
	assertBinding(t, polyglot, "core-utils", "npm", "pnpm")
	assertBinding(t, polyglot, "core-utils", "pypi", "uv")

	submodule := parseExample(t, "submodule-only.a2amodule.yml")
	assertBinding(t, submodule, "board-support", "submodule", "git")
	build := parseExample(t, "submodule-build.a2amodule.yml")
	assertBinding(t, build, "rendering", "submodule", "git")
	assertBinding(t, build, "rendering", "cmake", "add-subdirectory")

	if got := parseExample(t, "surface-directory.a2amodule.yml").Component.Surface; got != "public" {
		t.Errorf("directory surface = %q", got)
	}
	rootSurface := parseExample(t, "surface-root.a2amodule.yml")
	if rootSurface.Component.Surface != "." {
		t.Errorf("root surface = %q", rootSurface.Component.Surface)
	}
	if got := rootSurface.Component.Exports[0].Checksum; got == "" {
		t.Error("Zig export must retain checksum")
	}

	aliases := parseExample(t, "same-basename-aliases.a2amodule.yml")
	if len(aliases.Dependencies) != 2 || aliases.Dependencies[0].Name == aliases.Dependencies[1].Name {
		t.Fatal("equal repository basenames must retain distinct aliases")
	}
	for _, dependency := range aliases.Dependencies {
		if !strings.HasSuffix(dependency.Git, "/common.git") {
			t.Errorf("fixture repository %q does not share common.git basename", dependency.Git)
		}
	}
}

func TestSchemaOneIsOnlyAnExplicitInvalidMigrationFixture(t *testing.T) {
	paths, err := filepath.Glob("examples/invalid/*")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || filepath.Base(paths[0]) != "schema-1-migration.a2amodule.yml" {
		t.Fatalf("invalid fixtures = %v, want only schema-1 migration", paths)
	}
	_, err = manifest.Parse(read(t, paths[0]))
	var unsupported *manifest.UnsupportedSchemaError
	if !errors.As(err, &unsupported) || unsupported.Schema != 1 {
		t.Fatalf("schema 1 error = %v, want UnsupportedSchemaError", err)
	}
}

func TestUnknownFieldsAreRejectedByReferenceParser(t *testing.T) {
	_, err := manifest.Parse([]byte("schema: 2\ncomponent:\n  id: valid\n  legacy: true\n"))
	if err == nil || !strings.Contains(err.Error(), "field legacy not found") {
		t.Fatalf("unknown field error = %v", err)
	}
}

func parseExample(t *testing.T, name string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse(read(t, filepath.Join("examples", name)))
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return m
}

func assertBinding(t *testing.T, m *manifest.Manifest, dependency, adapter, variant string) {
	t.Helper()
	for _, d := range m.Dependencies {
		if d.Name != dependency {
			continue
		}
		for _, binding := range d.Bindings {
			if binding.Adapter == adapter && binding.Variant == variant {
				return
			}
		}
	}
	t.Errorf("missing %s/%s binding for %s", adapter, variant, dependency)
}

func fieldsFromHint(t *testing.T, body []byte) map[string][]string {
	t.Helper()
	fields := map[string][]string{}
	current := ""
	structure := regexp.MustCompile(`^<data_structure\b[^>]*\bname="([^"]*)"`)
	field := regexp.MustCompile(`^<field\b[^>]*\bname="([^"]*)"`)
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		line := scanner.Text()
		if match := structure.FindStringSubmatch(line); match != nil {
			current = match[1]
			continue
		}
		if line == "</data_structure>" {
			current = ""
			continue
		}
		if current != "" {
			if match := field.FindStringSubmatch(line); match != nil {
				fields[current] = append(fields[current], match[1])
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return fields
}

func readSchema(t *testing.T, path string) map[string]any {
	t.Helper()
	var schema map[string]any
	if err := json.Unmarshal(read(t, path), &schema); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return schema
}

func schemaFields(t *testing.T, schema map[string]any, definition string) []string {
	t.Helper()
	object := schema
	if definition != "" {
		object = schema["$defs"].(map[string]any)[definition].(map[string]any)
	}
	properties := object["properties"].(map[string]any)
	result := make([]string, 0, len(properties))
	for name := range properties {
		result = append(result, name)
	}
	return result
}

func goFields(value reflect.Type) []string {
	result := make([]string, 0, value.NumField())
	for i := 0; i < value.NumField(); i++ {
		name := strings.Split(value.Field(i).Tag.Get("yaml"), ",")[0]
		if name != "" && name != "-" {
			result = append(result, name)
		}
	}
	return result
}

func assertFields(t *testing.T, label string, want, got []string) {
	t.Helper()
	want = append([]string(nil), want...)
	got = append([]string(nil), got...)
	sort.Strings(want)
	sort.Strings(got)
	if !reflect.DeepEqual(want, got) {
		t.Errorf("%s fields differ: want %v, got %v", label, want, got)
	}
}

func assertClosedDefinition(t *testing.T, schema map[string]any, name string) {
	t.Helper()
	definition := schema["$defs"].(map[string]any)[name].(map[string]any)
	if definition["additionalProperties"] != false {
		t.Errorf("definition %s must reject unknown fields", name)
	}
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}
