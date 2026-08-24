package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestExamples(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "spec", "examples", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			var err error
			if strings.HasSuffix(path, ".lock") {
				_, err = LoadLock(path)
			} else {
				_, err = Load(path)
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInvalidExamplesNamePath(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "spec", "examples", "invalid", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 4 {
		t.Fatalf("got %d invalid examples", len(paths))
	}
	for _, path := range paths {
		_, err := Load(path)
		if err == nil {
			t.Errorf("%s unexpectedly valid", path)
			continue
		}
		if !strings.Contains(err.Error(), ".") && !strings.Contains(err.Error(), "schema") {
			t.Errorf("%s error does not identify a path: %v", path, err)
		}
	}
}

func TestLockDeterministic(t *testing.T) {
	l := &Lock{Schema: 1, Dependencies: map[string]LockedDependency{
		"z": {Git: "u", Ref: "main", Path: ".", Commit: strings.Repeat("a", 40), Manifest: "sha256:" + strings.Repeat("b", 64)},
		"a": {Git: "u", Ref: "main", Path: ".", Commit: strings.Repeat("c", 40), Manifest: "sha256:" + strings.Repeat("d", 64)},
	}}
	one, err := MarshalLock(l)
	if err != nil {
		t.Fatal(err)
	}
	two, err := MarshalLock(l)
	if err != nil {
		t.Fatal(err)
	}
	if string(one) != string(two) {
		t.Fatal("lock rendering is not deterministic")
	}
	if strings.Index(string(one), "a:") > strings.Index(string(one), "z:") {
		t.Fatal("dependency keys are not sorted")
	}
	if !strings.HasPrefix(string(one), "schema: 1\ndependencies:\n") {
		t.Fatalf("non-canonical top-level order:\n%s", one)
	}
}

func TestManifestExtension(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "spec", "examples", "acme-lib-utils.a2amodule.yml"))
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, []byte("x-test: yes\n")...)
	if _, err := Parse(b); err != nil {
		t.Fatal(err)
	}
}

func TestSpecManifestExamplesAreCanonical(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "spec", "examples", "*.a2amodule.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		original, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		formatted, formatErr := Format(original)
		if formatErr != nil {
			t.Fatal(formatErr)
		}
		if !bytes.Equal(formatted, original) {
			t.Errorf("%s is not canonical", filepath.Base(path))
		}
	}
}

func TestUpdateDependenciesPreservesCommentsStylesAndExtensions(t *testing.T) {
	original := []byte(`# manifest comment
schema: 1
module:
  id: consumer
  description: >-
    folded text stays folded
  languages: [go, python]
x-verbatim: "quoted value"
dependencies:
  - id: dep
    git: https://example.test/old.git
    ref: main # ref comment
    track: locked
    wire: [npm, pypi]
    x-private: "keep quoted"
`)
	wire := []string{"npm", "pypi"}
	updated, err := UpdateDependencies(original, []Dependency{{ID: "dep", Git: "https://example.test/new.git", Ref: "release", Track: "locked", Wire: &wire}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(updated)
	for _, preserved := range []string{"# manifest comment", "description: >-", "languages: [go, python]", `x-verbatim: "quoted value"`, "ref: release # ref comment", "wire: [npm, pypi]", `x-private: "keep quoted"`} {
		if !strings.Contains(text, preserved) {
			t.Errorf("missing preserved text %q:\n%s", preserved, text)
		}
	}
}

func TestUnknownContactKindCarriesArbitraryKeysThroughJSON(t *testing.T) {
	raw := []byte("schema: 1\nmodule: {id: consumer}\nagents:\n  - name: owner\n    role: owner\n    contacts:\n      - intents: [incident]\n        kind: pager-duty\n        service: checkout\n        escalation: 2\n        x-color: red\n")
	m, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	contact := m.Agents[0].Contacts[0]
	if contact.Extensions["service"] != "checkout" || contact.Extensions["escalation"] != 2 {
		t.Fatalf("extensions = %#v", contact.Extensions)
	}
	encoded, err := json.Marshal(contact)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"service":"checkout"`, `"escalation":2`, `"x-color":"red"`} {
		if !bytes.Contains(encoded, []byte(field)) {
			t.Errorf("JSON missing %s: %s", field, encoded)
		}
	}
}

func TestKnownContactKindRejectsAnotherKindsKeys(t *testing.T) {
	raw := []byte("schema: 1\nmodule: {id: consumer}\nagents:\n  - name: owner\n    role: owner\n    contacts:\n      - intents: [question]\n        kind: email\n        address: owner@example.test\n        project: WRONG\n")
	if _, err := Parse(raw); err == nil || !strings.Contains(err.Error(), "project: not valid for contact kind email") {
		t.Fatalf("error = %v", err)
	}
}

func TestDeclaredContactInvocationValidation(t *testing.T) {
	base := `schema: 1
module: {id: consumer}
agents:
  - name: owner
    role: owner
    contacts:
      - intents: [change]
        %s
`
	tests := map[string]struct {
		contact string
		want    string
	}{
		"header placeholder":    {"kind: http\n        url: https://tracker.example.test/issues\n        headers: {Authorization: 'Bearer {message}'}", "headers.Authorization: placeholders are not allowed"},
		"message in URL":        {"kind: http\n        url: https://tracker.example.test/issues?q={message}", "unsupported placeholder {message}"},
		"placeholder in path":   {"kind: http\n        url: https://tracker.example.test/{module}", "placeholders are allowed only in query values"},
		"non HTTPS":             {"kind: http\n        url: http://tracker.example.test/issues", "must be an https URL"},
		"exec path":             {"kind: exec\n        command: [./acme-tracker]", "command[0]: must be a bare binary name"},
		"exec command template": {"kind: exec\n        command: ['acme-{module}']", "command[0]: placeholders are not allowed"},
		"unknown placeholder":   {"kind: exec\n        command: [acme-tracker]\n        stdin: '{secret}'", "unsupported placeholder {secret}"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(fmt.Sprintf(base, test.contact)))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}

	valid := fmt.Sprintf(base, "kind: exec\n        command: [acme-tracker]\n        args: ['--module', '{module}', '--intent', '{intent}', '--literal', '{{word}}']\n        stdin: '{message}'")
	if _, err := Parse([]byte(valid)); err != nil {
		t.Fatalf("valid exec contact: %v", err)
	}
	validHTTP := fmt.Sprintf(base, `kind: http
        url: https://tracker.example.test/issues?module={module}
        content-type: application/json
        body: '{"module":"{module}","message":"{message}","literal":"{{word}}"}'`)
	if _, err := Parse([]byte(validHTTP)); err != nil {
		t.Fatalf("valid JSON HTTP contact: %v", err)
	}
	t.Log("JSON HTTP contact with structural braces: valid")
	typoHTTP := fmt.Sprintf(base, `kind: http
        url: https://tracker.example.test/issues
        content-type: application/json
        body: '{"value":"{typo}"}'`)
	if _, err := Parse([]byte(typoHTTP)); err == nil || !strings.Contains(err.Error(), "unsupported placeholder {typo}") {
		t.Fatalf("typo error = %v", err)
	} else {
		t.Log(err)
	}
}

func TestContactSettingsRequireConsumerSafeAllowlistValues(t *testing.T) {
	raw := []byte(`schema: 1
module: {id: consumer}
settings:
  contact:
    allow-http: [https://tracker.example.test]
    allow-exec: [acme-tracker]
`)
	if _, err := Parse(raw); err != nil {
		t.Fatal(err)
	}
	for name, replacement := range map[string]string{
		"HTTP origin": "http://tracker.example.test",
		"HTTP path":   "https://tracker.example.test/api",
		"exec path":   "./acme-tracker",
	} {
		t.Run(name, func(t *testing.T) {
			candidate := string(raw)
			if strings.HasPrefix(name, "HTTP") {
				candidate = strings.Replace(candidate, "https://tracker.example.test", replacement, 1)
			} else {
				candidate = strings.Replace(candidate, "acme-tracker", replacement, 1)
			}
			if _, err := Parse([]byte(candidate)); err == nil {
				t.Fatal("manifest unexpectedly valid")
			}
		})
	}
}

func TestVendorValidationAndLockShape(t *testing.T) {
	valid := []byte(`schema: 1
module:
  id: consumer
  surface: surface
settings:
  vendor-dir: third_party
  sync-targets: [CLAUDE.md]
dependencies:
  - id: acme-lib
    git: https://example.test/acme-lib.git
    vendor:
      mode: copy
      path: third_party/acme-lib
`)
	if _, err := Parse(valid); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"copy recursive":  strings.Replace(string(valid), "path: third_party/acme-lib", "path: third_party/acme-lib\n      recursive: true", 1),
		"metadata path":   strings.Replace(string(valid), "third_party/acme-lib", ".git/modules/acme-lib", 1),
		"surface overlap": strings.Replace(string(valid), "third_party/acme-lib", "surface/generated", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(body)); err == nil {
				t.Fatal("manifest unexpectedly valid")
			}
		})
	}

	lock := &Lock{Schema: 1, Dependencies: map[string]LockedDependency{
		"acme-lib": {
			Git: "https://example.test/acme-lib.git", Ref: "main", Path: ".",
			Commit: strings.Repeat("a", 40), Manifest: "sha256:" + strings.Repeat("b", 64),
			Vendor: &LockedVendor{Mode: "copy", Path: "third_party/acme-lib", Tree: "tree:" + strings.Repeat("c", 40)},
		},
	}}
	encoded, err := MarshalLock(lock)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip Lock
	if err = yaml.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if err = roundTrip.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateDependenciesPreservesVendorCollectionStyle(t *testing.T) {
	original := []byte("schema: 1\nmodule: {id: consumer}\ndependencies:\n  - id: acme-lib\n    git: old\n    vendor: {mode: submodule, path: deps/acme-lib} # keep\n")
	updated, err := UpdateDependencies(original, []Dependency{{
		ID: "acme-lib", Git: "new", Vendor: &Vendor{Mode: "copy", Path: "third_party/acme-lib"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "vendor: {mode: copy, path: third_party/acme-lib} # keep") {
		t.Fatalf("vendor style/comment lost:\n%s", updated)
	}
}
