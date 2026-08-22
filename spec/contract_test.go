package spec_test

import (
	"bufio"
	"encoding/json"
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

func TestHintEntityFieldsMatchSchemaAndGoTypes(t *testing.T) {
	root := filepath.Clean("..")
	command := exec.Command("hint", "spec")
	command.Dir = root
	compiled, err := command.Output()
	if err != nil {
		t.Fatalf("compile hint spec: %v", err)
	}
	specFields := fieldsFromCompiledHint(t, compiled)

	rawSchema, err := os.ReadFile(filepath.Join(root, "spec", "schema", "a2amodule.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err = json.Unmarshal(rawSchema, &schema); err != nil {
		t.Fatal(err)
	}

	types := map[string]reflect.Type{
		"manifest":   reflect.TypeOf(manifest.Manifest{}),
		"module":     reflect.TypeOf(manifest.Module{}),
		"agent":      reflect.TypeOf(manifest.Agent{}),
		"policy":     reflect.TypeOf(manifest.Policy{}),
		"dependency": reflect.TypeOf(manifest.Dependency{}),
	}
	definitions := map[string]string{"manifest": "", "module": "module", "agent": "agent", "policy": "policy", "dependency": "dependency"}
	for entity, goType := range types {
		want, ok := specFields[entity]
		if !ok {
			t.Errorf("compiled hint spec has no entity %q", entity)
			continue
		}
		want = withoutExtensionWildcard(want)
		assertSameFields(t, entity+" schema", want, schemaFields(t, schema, definitions[entity]))
		assertSameFields(t, entity+" Go type", want, goFields(goType))
	}
}

func fieldsFromCompiledHint(t *testing.T, body []byte) map[string][]string {
	t.Helper()
	fields := map[string][]string{}
	current := ""
	dataStructure := regexp.MustCompile(`^<data_structure\b[^>]*\bid="([^"]*)"`)
	field := regexp.MustCompile(`^<field\b[^>]*\bname="([^"]*)"`)
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		line := scanner.Text()
		if match := dataStructure.FindStringSubmatch(line); match != nil {
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
		t.Fatalf("scan compiled hint XML: %v", err)
	}
	return fields
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
	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		if field.Name == "Extensions" {
			continue
		}
		name := strings.Split(field.Tag.Get("yaml"), ",")[0]
		if name != "" && name != "-" {
			result = append(result, name)
		}
	}
	return result
}

func withoutExtensionWildcard(fields []string) []string {
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		if field != "x-*" {
			result = append(result, field)
		}
	}
	return result
}

func assertSameFields(t *testing.T, label string, want, got []string) {
	t.Helper()
	sort.Strings(want)
	sort.Strings(got)
	if !reflect.DeepEqual(want, got) {
		t.Errorf("%s fields differ\ncompiled hint: %v\nimplementation: %v", label, want, got)
	}
}
