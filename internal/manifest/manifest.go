package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	hashPattern   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	treePattern   = regexp.MustCompile(`^tree:[0-9a-f]{40}$`)
)

func Load(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

func Parse(b []byte) (*Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
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
	var l Lock
	if err := yaml.Unmarshal(b, &l); err != nil {
		return nil, err
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return &l, nil
}

func (m *Manifest) Validate() error {
	var errs []error
	if m.Schema != 1 {
		errs = append(errs, fmt.Errorf("schema: must equal 1"))
	}
	if !idPattern.MatchString(m.Module.ID) {
		errs = append(errs, fmt.Errorf("module.id: must match %s", idPattern))
	}
	validateExtensions("", m.Extensions, &errs)
	validateExtensions("module", m.Module.Extensions, &errs)
	validateRelative("module.surface", m.Module.Surface, &errs)
	if m.Module.Release != nil {
		validateExtensions("module.release", m.Module.Release.Extensions, &errs)
	}
	for i, e := range m.Module.Exports {
		p := fmt.Sprintf("module.exports[%d]", i)
		if e.Ecosystem == "" {
			errs = append(errs, fmt.Errorf("%s.ecosystem: required", p))
		}
		if e.Name == "" {
			errs = append(errs, fmt.Errorf("%s.name: required", p))
		}
		validateRelative(p+".path", e.Path, &errs)
		validateExtensions(p, e.Extensions, &errs)
	}
	for i, a := range m.Agents {
		p := fmt.Sprintf("agents[%d]", i)
		if a.Name == "" {
			errs = append(errs, fmt.Errorf("%s.name: required", p))
		}
		if a.Role == "" {
			errs = append(errs, fmt.Errorf("%s.role: required", p))
		}
		for j, scope := range a.Scope {
			validateRelative(fmt.Sprintf("%s.scope[%d]", p, j), scope, &errs)
		}
		validateExtensions(p, a.Extensions, &errs)
		if a.Trust != nil {
			validateExtensions(p+".trust", a.Trust.Extensions, &errs)
		}
		for j, c := range a.Contacts {
			cp := fmt.Sprintf("%s.contacts[%d]", p, j)
			if len(c.Intents) == 0 {
				errs = append(errs, fmt.Errorf("%s.intents: required and must not be empty", cp))
			}
			if c.Kind == "" {
				errs = append(errs, fmt.Errorf("%s.kind: required", cp))
			}
			validateExtensions(cp, c.Extensions, &errs)
		}
	}
	if m.Policy != nil {
		validateExtensions("policy", m.Policy.Extensions, &errs)
		if m.Policy.Consumers != nil {
			validateExtensions("policy.consumers", m.Policy.Consumers.Extensions, &errs)
		}
	}
	seen := map[string]bool{}
	for i, d := range m.Dependencies {
		p := fmt.Sprintf("dependencies[%d]", i)
		if d.ID != "" && !idPattern.MatchString(d.ID) {
			errs = append(errs, fmt.Errorf("%s.id: must match %s", p, idPattern))
		}
		if d.ID != "" && seen[d.ID] {
			errs = append(errs, fmt.Errorf("%s.id: duplicate %q", p, d.ID))
		}
		seen[d.ID] = true
		if d.Git == "" {
			errs = append(errs, fmt.Errorf("%s.git: required", p))
		}
		if d.Track != "" && d.Track != "locked" && d.Track != "floating" {
			errs = append(errs, fmt.Errorf("%s.track: must be locked or floating", p))
		}
		validateRelative(p+".path", d.Path, &errs)
		validateExtensions(p, d.Extensions, &errs)
	}
	return errors.Join(errs...)
}

func (l *Lock) Validate() error {
	var errs []error
	if l.Schema != 1 {
		errs = append(errs, fmt.Errorf("schema: must equal 1"))
	}
	if l.Dependencies == nil {
		errs = append(errs, fmt.Errorf("dependencies: required"))
	}
	validateExtensions("", l.Extensions, &errs)
	for id, d := range l.Dependencies {
		p := "dependencies." + id
		if !idPattern.MatchString(id) {
			errs = append(errs, fmt.Errorf("%s: invalid dependency id", p))
		}
		if d.Git == "" {
			errs = append(errs, fmt.Errorf("%s.git: required", p))
		}
		if d.Ref == "" {
			errs = append(errs, fmt.Errorf("%s.ref: required", p))
		}
		if d.Path == "" {
			errs = append(errs, fmt.Errorf("%s.path: required", p))
		} else {
			validateRelative(p+".path", d.Path, &errs)
		}
		if !commitPattern.MatchString(d.Commit) {
			errs = append(errs, fmt.Errorf("%s.commit: must be 40 lowercase hex characters", p))
		}
		if !hashPattern.MatchString(d.Manifest) {
			errs = append(errs, fmt.Errorf("%s.manifest: invalid sha256 hash", p))
		}
		for name, hash := range d.Cards {
			if !hashPattern.MatchString(hash) {
				errs = append(errs, fmt.Errorf("%s.cards.%s: invalid sha256 hash", p, name))
			}
		}
		if d.Surface != "" && !treePattern.MatchString(d.Surface) {
			errs = append(errs, fmt.Errorf("%s.surface: invalid tree id", p))
		}
		validateExtensions(p, d.Extensions, &errs)
	}
	return errors.Join(errs...)
}

func validateExtensions(path string, values map[string]any, errs *[]error) {
	for key := range values {
		if !strings.HasPrefix(key, "x-") {
			if path != "" {
				key = path + "." + key
			}
			*errs = append(*errs, fmt.Errorf("%s: unknown key", key))
		}
	}
}

func validateRelative(path, value string, errs *[]error) {
	if value == "" {
		return
	}
	normal := strings.ReplaceAll(value, `\`, "/")
	if filepath.IsAbs(value) || strings.HasPrefix(normal, "../") || strings.Contains(normal, "/../") || normal == ".." {
		*errs = append(*errs, fmt.Errorf("%s: must be a relative path without ..", path))
	}
}

func Marshal(m *Manifest) ([]byte, error) {
	b, err := yaml.Marshal(m)
	if err != nil {
		return nil, err
	}
	return bytes.TrimRight(b, "\n")[:], nil
}

func MarshalLock(l *Lock) ([]byte, error) {
	var node yaml.Node
	b, err := yaml.Marshal(l)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, &node); err != nil {
		return nil, err
	}
	if len(node.Content) > 0 {
		sortMapNode(node.Content[0])
	}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&node); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func sortMapNode(n *yaml.Node) {
	if n.Kind == yaml.MappingNode {
		pairs := make([][2]*yaml.Node, 0, len(n.Content)/2)
		for i := 0; i < len(n.Content); i += 2 {
			pairs = append(pairs, [2]*yaml.Node{n.Content[i], n.Content[i+1]})
		}
		sort.SliceStable(pairs, func(i, j int) bool { return pairs[i][0].Value < pairs[j][0].Value })
		n.Content = n.Content[:0]
		for _, p := range pairs {
			n.Content = append(n.Content, p[0], p[1])
			sortMapNode(p[1])
		}
	} else {
		for _, child := range n.Content {
			sortMapNode(child)
		}
	}
}
