package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitInterviewJSONReportsOrderedComputedFacts(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/acme/tool\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("instructions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"init", "--interview", "--json"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var spec interviewSpec
	if err := json.Unmarshal(out.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	want := []string{"module.id", "module.description", "module.repository", "module.languages", "module.exports"}
	for i, path := range want {
		if spec.Questions[i].FieldPath != path {
			t.Fatalf("question %d = %q", i, spec.Questions[i].FieldPath)
		}
	}
	if spec.Questions[0].Confidence != "low" || spec.Questions[1].Confidence != "low" {
		t.Fatal("id and description must remain low-confidence")
	}
	if got := spec.Questions[4].Default.([]any)[0].(map[string]any)["name"]; got != "github.com/acme/tool" {
		t.Fatalf("export = %v", got)
	}
	if len(spec.SetupHints) != 1 || !strings.Contains(spec.SetupHints[0], "AGENTS.md detected") {
		t.Fatalf("setup hints = %#v", spec.SetupHints)
	}
}

func TestInitAnswersAndTTYProduceByteIdenticalManifest(t *testing.T) {
	answers := `{
  "module.id": "acme-tool",
  "module.description": "Useful tool",
  "module.repository": "https://github.com/acme/tool",
  "module.languages": ["go"],
  "module.exports": [{"ecosystem":"golang","name":"github.com/acme/tool"}]
}`
	answersRoot := t.TempDir()
	answersPath := filepath.Join(answersRoot, "answers.json")
	if err := os.WriteFile(answersPath, []byte(answers), 0o644); err != nil {
		t.Fatal(err)
	}
	var out1, err1 bytes.Buffer
	app1 := New(&out1, &err1)
	app1.Root = answersRoot
	if code := app1.Run([]string{"init", "--answers", answersPath}); code != 0 {
		t.Fatalf("answers exit %d: %s", code, err1.String())
	}
	want, _ := os.ReadFile(filepath.Join(answersRoot, "a2amodule.yml"))

	ttyRoot := t.TempDir()
	interactive := true
	input := strings.NewReader("acme-tool\nUseful tool\nhttps://github.com/acme/tool\n[\"go\"]\n[{\"ecosystem\":\"golang\",\"name\":\"github.com/acme/tool\"}]\ny\n")
	var out2, err2 bytes.Buffer
	app2 := New(&out2, &err2)
	app2.Root = ttyRoot
	app2.In = input
	app2.Interactive = &interactive
	if code := app2.Run([]string{"init"}); code != 0 {
		t.Fatalf("tty exit %d: %s", code, err2.String())
	}
	got, _ := os.ReadFile(filepath.Join(ttyRoot, "a2amodule.yml"))
	if !bytes.Equal(got, want) {
		t.Fatalf("manifests differ\nanswers:\n%s\ntty:\n%s", want, got)
	}
	if !strings.Contains(out2.String(), "Manifest preview:") {
		t.Fatalf("missing preview: %s", out2.String())
	}
}

func TestInitAnswersRejectUnknownKeysInSortedOrder(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "answers.yml")
	if err := os.WriteFile(path, []byte("z.field: x\na.field: y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"init", "--answers", path}); code != 2 {
		t.Fatalf("exit %d", code)
	}
	if got := strings.TrimSpace(errOut.String()); got != "init: unknown answer keys: a.field, z.field" {
		t.Fatalf("stderr = %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "a2amodule.yml")); !os.IsNotExist(err) {
		t.Fatalf("manifest written: %v", err)
	}
}

func TestInitNonTTYUsesDefaultsAndNamesAnswers(t *testing.T) {
	root := t.TempDir()
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	app.In = strings.NewReader("")
	if code := app.Run([]string{"init"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "accepting computed defaults") || !strings.Contains(errOut.String(), "--answers FILE|-") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestDependencyIDFromWindowsFileURLUsesRepositoryBasename(t *testing.T) {
	if got := dependencyIDFromURL(`file://C:\Users\runner\repo\acme-plain.git`); got != "acme-plain" {
		t.Fatalf("id = %q", got)
	}
}
