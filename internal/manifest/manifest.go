package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	CurrentSchema = 2
	CanonicalName = "a2amodule.yml"
	AlternateName = "a2amodule.yaml"
)

var idPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?$`)
var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type UnsupportedSchemaError struct{ Schema int }

func (e *UnsupportedSchemaError) Error() string {
	return fmt.Sprintf("schema %d is not supported; git-a2a requires schema 2 (see docs/migration-v2.md)", e.Schema)
}

func Path(root string) (string, error) {
	yml := filepath.Join(root, CanonicalName)
	yamlPath := filepath.Join(root, AlternateName)
	_, ymlErr := os.Stat(yml)
	_, yamlErr := os.Stat(yamlPath)
	if ymlErr == nil && yamlErr == nil {
		return "", fmt.Errorf("exactly one of %s or %s may exist", CanonicalName, AlternateName)
	}
	if ymlErr == nil {
		return yml, nil
	}
	if yamlErr == nil {
		return yamlPath, nil
	}
	if !os.IsNotExist(ymlErr) {
		return "", ymlErr
	}
	if !os.IsNotExist(yamlErr) {
		return "", yamlErr
	}
	return "", os.ErrNotExist
}

func ReadDir(root string) ([]byte, string, error) {
	p, err := Path(root)
	if err != nil {
		return nil, "", err
	}
	b, err := os.ReadFile(p)
	return b, p, err
}

func LoadDir(root string) (*Manifest, error) {
	p, err := Path(root)
	if err != nil {
		return nil, err
	}
	return Load(p)
}
func Load(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

func Parse(b []byte) (*Manifest, error) {
	var probe struct {
		Schema int `yaml:"schema"`
	}
	if err := yaml.Unmarshal(b, &probe); err != nil {
		return nil, err
	}
	if probe.Schema != CurrentSchema {
		return nil, &UnsupportedSchemaError{Schema: probe.Schema}
	}
	var m Manifest
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func LoadLock(path string) (*Lock, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var probe struct {
		Schema int `yaml:"schema"`
	}
	if err := yaml.Unmarshal(b, &probe); err != nil {
		return nil, err
	}
	if probe.Schema != CurrentSchema {
		return nil, &UnsupportedSchemaError{Schema: probe.Schema}
	}
	var l Lock
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&l); err != nil {
		return nil, err
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return &l, nil
}

func (m *Manifest) Validate() error {
	if m.Schema != CurrentSchema {
		return &UnsupportedSchemaError{Schema: m.Schema}
	}
	var errs []error
	if !idPattern.MatchString(m.Component.ID) {
		errs = append(errs, fmt.Errorf("component.id: must match %s", idPattern))
	}
	validateRelative("component.surface", m.Component.Surface, true, &errs)
	if m.Agent != nil {
		if strings.TrimSpace(m.Agent.Card) == "" {
			errs = append(errs, fmt.Errorf("agent.card: required when agent is declared"))
		} else {
			validateCard("agent.card", m.Agent.Card, &errs)
		}
	}
	exports := map[string]bool{}
	for i, e := range m.Component.Exports {
		p := fmt.Sprintf("component.exports[%d]", i)
		if e.Adapter == "" {
			errs = append(errs, fmt.Errorf("%s.adapter: required", p))
		}
		if e.Name == "" {
			errs = append(errs, fmt.Errorf("%s.name: required", p))
		}
		validateRelative(p+".path", e.Path, false, &errs)
		key := e.Adapter + "\x00" + e.Name
		if exports[key] {
			errs = append(errs, fmt.Errorf("%s: duplicate adapter/name", p))
		}
		exports[key] = true
	}
	seen := map[string]bool{}
	claimedBindings := map[string]string{}
	for i, d := range m.Dependencies {
		p := fmt.Sprintf("dependencies[%d]", i)
		if !idPattern.MatchString(d.Name) {
			errs = append(errs, fmt.Errorf("%s.name: must match %s", p, idPattern))
		}
		if seen[d.Name] {
			errs = append(errs, fmt.Errorf("%s.name: duplicate %q", p, d.Name))
		}
		seen[d.Name] = true
		if strings.TrimSpace(d.Git) == "" {
			errs = append(errs, fmt.Errorf("%s.git: required", p))
		}
		validateRelative(p+".path", d.Path, false, &errs)
		validateBindings(p+".bindings", d.Bindings, &errs)
		for j, binding := range d.Bindings {
			key := binding.Adapter + "\x00" + binding.Variant + "\x00" + binding.Export
			if owner := claimedBindings[key]; owner != "" {
				errs = append(errs, fmt.Errorf("%s.bindings[%d]: adapter/variant/export is already claimed by dependency %s", p, j, owner))
			} else {
				claimedBindings[key] = d.Name
			}
		}
	}
	return errors.Join(errs...)
}

func (l *Lock) Validate() error {
	if l.Schema != CurrentSchema {
		return &UnsupportedSchemaError{Schema: l.Schema}
	}
	var errs []error
	for name, d := range l.Dependencies {
		p := "dependencies." + name
		if !idPattern.MatchString(name) {
			errs = append(errs, fmt.Errorf("%s: invalid alias", p))
		}
		if d.Git == "" || d.Ref == "" || d.Component == "" {
			errs = append(errs, fmt.Errorf("%s: component, git, and ref are required", p))
		}
		if !commitPattern.MatchString(d.Commit) {
			errs = append(errs, fmt.Errorf("%s.commit: must be a 40-character lowercase Git object ID", p))
		}
		if !digestPattern.MatchString(d.Manifest) {
			errs = append(errs, fmt.Errorf("%s.manifest: must be a sha256 digest", p))
		}
		validateLockedAgent(p+".agent", name, d, &errs)
		validateBindings(p+".bindings", d.Bindings, &errs)
	}
	return errors.Join(errs...)
}

func validateBindings(prefix string, bindings []Binding, errs *[]error) {
	seen := map[string]bool{}
	for i, b := range bindings {
		p := fmt.Sprintf("%s[%d]", prefix, i)
		if b.Adapter == "" || b.Variant == "" {
			*errs = append(*errs, fmt.Errorf("%s: adapter and variant are required", p))
		}
		key := b.Adapter + "\x00" + b.Variant + "\x00" + b.Export
		if seen[key] {
			*errs = append(*errs, fmt.Errorf("%s: duplicate adapter/variant/export", p))
		}
		seen[key] = true
		validateRelative(p+".path", b.Path, false, errs)
	}
}

func validateCard(name, value string, errs *[]error) {
	if strings.HasPrefix(value, "./") || (!strings.Contains(value, "://") && !strings.HasPrefix(value, "/")) {
		validateRelative(name, value, false, errs)
		return
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		*errs = append(*errs, fmt.Errorf("%s: must be an https URL or repository-relative path", name))
	}
}

func validateLockedAgent(prefix, alias string, dependency LockedDependency, errs *[]error) {
	agent := dependency.Agent
	if strings.TrimSpace(agent.DeclaredCard) == "" {
		*errs = append(*errs, fmt.Errorf("%s.declaredCard: required", prefix))
		return
	}
	validateCard(prefix+".declaredCard", agent.DeclaredCard, errs)
	if agent.Card == "" {
		*errs = append(*errs, fmt.Errorf("%s.card: required", prefix))
	}
	if !commitPattern.MatchString(agent.Commit) {
		*errs = append(*errs, fmt.Errorf("%s.commit: must be a 40-character lowercase Git object ID", prefix))
	} else if agent.Commit != dependency.Commit {
		*errs = append(*errs, fmt.Errorf("%s.commit: must equal dependency commit", prefix))
	}
	if isHTTPSCard(agent.DeclaredCard) {
		if agent.Card != agent.DeclaredCard {
			*errs = append(*errs, fmt.Errorf("%s.card: HTTPS cards must retain the declared reference", prefix))
		}
		return
	}
	validateRelative(prefix+".card", agent.Card, false, errs)
	want := filepath.ToSlash(filepath.Join(".git-a2a", "agents", alias, "agent-card.json"))
	if agent.Card != want {
		*errs = append(*errs, fmt.Errorf("%s.card: repository-relative cards must resolve to %s", prefix, want))
	}
}

func isHTTPSCard(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Host != ""
}

func validateRelative(name, value string, allowDot bool, errs *[]error) {
	if value == "" {
		return
	}
	clean := filepath.ToSlash(filepath.Clean(value))
	if clean == "." && allowDot {
		return
	}
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || filepath.IsAbs(value) || strings.Contains(value, "\\") {
		*errs = append(*errs, fmt.Errorf("%s: must be a safe repository-relative path", name))
	}
}

func Marshal(m *Manifest) ([]byte, error) { return yaml.Marshal(m) }
func MarshalLock(l *Lock) ([]byte, error) { return yaml.Marshal(l) }
func Sort(m *Manifest) {
	sort.SliceStable(m.Dependencies, func(i, j int) bool { return m.Dependencies[i].Name < m.Dependencies[j].Name })
}
func DependencyByName(m *Manifest, name string) (*Dependency, int) {
	for i := range m.Dependencies {
		if m.Dependencies[i].Name == name {
			return &m.Dependencies[i], i
		}
	}
	return nil, -1
}
