package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	lockfile "github.com/neprel/git-a2a/internal/lock"
	"github.com/neprel/git-a2a/internal/manifest"
	"gopkg.in/yaml.v3"
)

type initRequest struct {
	ID, Description, Surface, Example, Answers string
	Exports                                    []string
	Interview, JSON                            bool
	IDExplicit                                 bool
}

type interviewQuestion struct {
	FieldPath  string `json:"fieldPath" yaml:"field-path"`
	Prompt     string `json:"prompt" yaml:"prompt"`
	Why        string `json:"why" yaml:"why"`
	Default    any    `json:"default" yaml:"default"`
	Confidence string `json:"confidence" yaml:"confidence"`
	Validation string `json:"validation" yaml:"validation"`
}

type interviewSpec struct {
	Questions  []interviewQuestion `json:"questions" yaml:"questions"`
	SetupHints []string            `json:"setupHints,omitempty" yaml:"setup-hints,omitempty"`
}

func (a *App) inputIsTerminal() bool {
	if a.Interactive != nil {
		return *a.Interactive
	}
	in := a.In
	if in == nil {
		return false
	}
	file, ok := in.(*os.File)
	if !ok {
		return false
	}
	return fileIsTerminal(file)
}

func (a *App) initSpec(req initRequest) interviewSpec {
	root := a.root()
	id := req.ID
	if id == "" {
		abs, _ := filepath.Abs(root)
		id = sanitizeID(strings.ToLower(filepath.Base(abs)))
	}
	repository := ""
	if out, err := a.runner().Run(a.context(), root, nil, "config", "--get", "remote.origin.url"); err == nil {
		repository = strings.TrimSpace(string(out))
	}
	exports := detectExports(root)
	languages := make([]string, 0, len(exports))
	seen := map[string]bool{}
	for _, export := range exports {
		language := ecosystemLanguage(export.Ecosystem)
		if language != "" && !seen[language] {
			seen[language] = true
			languages = append(languages, language)
		}
	}
	questions := []interviewQuestion{
		{FieldPath: "module.id", Prompt: "Module id", Why: "Stable identity used by locks, routing, and dependency declarations.", Default: id, Confidence: "low", Validation: "lowercase module id: letters, digits, dot, underscore, or hyphen"},
		{FieldPath: "module.description", Prompt: "Description", Why: "Explains the module to humans and consuming agents.", Default: req.Description, Confidence: "low", Validation: "string"},
		{FieldPath: "module.repository", Prompt: "Canonical repository", Why: "Lets consumers distinguish the canonical source from a fork.", Default: repository, Confidence: confidence(repository != ""), Validation: "git URL or empty"},
		{FieldPath: "module.languages", Prompt: "Languages", Why: "Records the implementation languages without choosing package-manager behavior.", Default: languages, Confidence: confidence(len(languages) > 0), Validation: "list of strings"},
		{FieldPath: "module.exports", Prompt: "Exports", Why: "Declares the native dependency coordinates consumers can wire.", Default: exports, Confidence: confidence(len(exports) > 0), Validation: "list of {ecosystem,name,path?,notes?}"},
	}
	var hints []string
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			hints = append(hints, name+" detected; run git-a2a setup after init")
		}
	}
	return interviewSpec{Questions: questions, SetupHints: hints}
}

func confidence(detected bool) string {
	if detected {
		return "high"
	}
	return "low"
}

func ecosystemLanguage(ecosystem string) string {
	switch ecosystem {
	case "npm":
		return "typescript"
	case "pypi":
		return "python"
	case "golang":
		return "go"
	case "cargo":
		return "rust"
	case "swift":
		return "swift"
	case "pub":
		return "dart"
	default:
		return ecosystem
	}
}

func (a *App) runInitInterview(req initRequest) int {
	spec := a.initSpec(req)
	if req.Interview {
		if req.JSON {
			body, _ := json.MarshalIndent(spec, "", "  ")
			fmt.Fprintln(a.Out, string(body))
		} else {
			for _, q := range spec.Questions {
				def, _ := json.Marshal(q.Default)
				fmt.Fprintf(a.Out, "%s\n  prompt: %s\n  why: %s\n  default: %s\n  confidence: %s\n  validation: %s\n", q.FieldPath, q.Prompt, q.Why, def, q.Confidence, q.Validation)
			}
			for _, hint := range spec.SetupHints {
				fmt.Fprintf(a.Out, "setup: %s\n", hint)
			}
		}
		return 0
	}
	answers := map[string]any{}
	var terminalReader *bufio.Reader
	if req.Answers != "" {
		body, err := a.readInitAnswers(req.Answers)
		if err != nil {
			fmt.Fprintf(a.Err, "init: answers: %v\n", err)
			return 2
		}
		if err := yaml.Unmarshal(body, &answers); err != nil {
			fmt.Fprintf(a.Err, "init: answers: %v\n", err)
			return 2
		}
		if unknown := unknownAnswers(spec, answers); len(unknown) > 0 {
			fmt.Fprintf(a.Err, "init: unknown answer keys: %s\n", strings.Join(unknown, ", "))
			return 2
		}
	} else if a.inputIsTerminal() && !a.yes {
		terminalReader = bufio.NewReader(inputReader(a.In))
		answers = a.askInitQuestions(spec, terminalReader)
		if answers == nil {
			return 2
		}
	}
	for _, q := range spec.Questions {
		if _, ok := answers[q.FieldPath]; !ok {
			answers[q.FieldPath] = q.Default
		}
	}
	if req.IDExplicit {
		answers["module.id"] = req.ID
	}
	if req.Description != "" {
		answers["module.description"] = req.Description
	}
	if req.Surface != "" {
		answers["module.surface"] = req.Surface
	}
	if len(req.Exports) > 0 {
		var exports []manifest.Export
		for _, item := range req.Exports {
			parts := strings.SplitN(item, "=", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				fmt.Fprintf(a.Err, "invalid --export %q\n", item)
				return 2
			}
			exports = append(exports, manifest.Export{Ecosystem: parts[0], Name: parts[1]})
		}
		answers["module.exports"] = exports
	}
	m, err := manifestFromInterview(req.Example, answers)
	if err != nil {
		fmt.Fprintf(a.Err, "init: answers: %v\n", err)
		return 2
	}
	body, _ := manifest.Marshal(m)
	if a.inputIsTerminal() && req.Answers == "" && !a.yes {
		fmt.Fprintf(a.Out, "\nManifest preview:\n%s", body)
		fmt.Fprint(a.Err, "Write a2amodule.yml? [Y/n] ")
		line, _ := terminalReader.ReadString('\n')
		line = strings.ToLower(strings.TrimSpace(line))
		if line == "n" || line == "no" {
			fmt.Fprintln(a.Err, "init: cancelled")
			return 2
		}
	}
	path := filepath.Join(a.root(), manifest.CanonicalName)
	if err := lockfile.Atomic(path, body, 0o644); err != nil {
		fmt.Fprintf(a.Err, "init: %v\n", err)
		return 1
	}
	if err := ensureIgnored(a.root()); err != nil {
		fmt.Fprintf(a.Err, "init: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Err, "initialized module %s\n", m.Module.ID)
	return 0
}

func inputReader(reader io.Reader) io.Reader {
	if reader == nil {
		return os.Stdin
	}
	return reader
}

func (a *App) readInitAnswers(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(inputReader(a.In))
	}
	return os.ReadFile(path)
}

func unknownAnswers(spec interviewSpec, answers map[string]any) []string {
	known := map[string]bool{"module.surface": true}
	for _, q := range spec.Questions {
		known[q.FieldPath] = true
	}
	var unknown []string
	for key := range answers {
		if !known[key] {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	return unknown
}

func (a *App) askInitQuestions(spec interviewSpec, reader *bufio.Reader) map[string]any {
	answers := map[string]any{}
	for _, q := range spec.Questions {
		for {
			fmt.Fprint(a.Err, initQuestionPrompt(q))
			line, err := reader.ReadString('\n')
			if err != nil && len(line) == 0 {
				fmt.Fprintln(a.Err, "init: input ended")
				return nil
			}
			line = strings.TrimSpace(line)
			if line == "?" {
				fmt.Fprintln(a.Err, q.Why)
				continue
			}
			if line == "" {
				answers[q.FieldPath] = q.Default
				break
			}
			var value any
			if strings.HasPrefix(line, "[") || strings.HasPrefix(line, "{") {
				if err := json.Unmarshal([]byte(line), &value); err != nil {
					fmt.Fprintf(a.Err, "invalid value: %v\n", err)
					continue
				}
			} else {
				value = line
			}
			answers[q.FieldPath] = value
			break
		}
	}
	return answers
}

func initQuestionPrompt(q interviewQuestion) string {
	if value, ok := q.Default.(string); ok {
		if value == "" {
			return q.Prompt + ": "
		}
		return fmt.Sprintf("%s (%s): ", q.Prompt, value)
	}
	if q.FieldPath == "module.exports" {
		if exports, ok := q.Default.([]manifest.Export); ok && len(exports) > 0 {
			ecosystems := make([]string, 0, len(exports))
			for _, export := range exports {
				ecosystems = append(ecosystems, export.Ecosystem)
			}
			return fmt.Sprintf("%s (detected: %s): ", q.Prompt, strings.Join(ecosystems, ", "))
		}
	}
	def, _ := json.Marshal(q.Default)
	if string(def) == "[]" || string(def) == "null" {
		return q.Prompt + ": "
	}
	return fmt.Sprintf("%s (%s): ", q.Prompt, def)
}

func manifestFromInterview(example string, answers map[string]any) (*manifest.Manifest, error) {
	var m *manifest.Manifest
	var err error
	if example != "" {
		m, err = manifest.Parse(exampleManifest(example, stringValue(answers["module.id"])))
	} else {
		m = &manifest.Manifest{Schema: manifest.CurrentSchema}
	}
	if err != nil {
		return nil, err
	}
	m.Module.ID = stringValue(answers["module.id"])
	m.Module.Description = stringValue(answers["module.description"])
	m.Module.Repository = stringValue(answers["module.repository"])
	m.Module.Surface = stringValue(answers["module.surface"])
	if value, ok := answers["module.languages"]; ok {
		body, _ := yaml.Marshal(value)
		if err := yaml.Unmarshal(body, &m.Module.Languages); err != nil {
			return nil, fmt.Errorf("module.languages: %w", err)
		}
	}
	if value, ok := answers["module.exports"]; ok {
		body, _ := yaml.Marshal(value)
		if err := yaml.Unmarshal(body, &m.Module.Exports); err != nil {
			return nil, fmt.Errorf("module.exports: %w", err)
		}
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return m, nil
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
