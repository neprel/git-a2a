package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	if m.Module.MovedTo != nil {
		if m.Module.MovedTo.Git == "" {
			errs = append(errs, fmt.Errorf("module.moved-to.git: required"))
		}
		validateRelative("module.moved-to.path", m.Module.MovedTo.Path, &errs)
		validateExtensions("module.moved-to", m.Module.MovedTo.Extensions, &errs)
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
			validateContact(cp, c, &errs)
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

func validateContact(path string, contact Contact, errs *[]error) {
	allowed := map[string]map[string]bool{
		"a2a":          {"url": true, "skill": true},
		"email":        {"address": true, "subject-prefix": true},
		"github-issue": {"repo": true, "labels": true, "template": true},
		"gitlab-issue": {"repo": true, "labels": true, "template": true},
		"jira":         {"url": true, "project": true, "issue-type": true},
		"mattermost":   {"channel": true, "handle": true, "server": true},
		"slack":        {"channel": true, "handle": true, "server": true},
		"discord":      {"channel": true, "handle": true, "server": true},
		"telegram":     {"channel": true, "handle": true, "server": true},
		"teams":        {"channel": true, "handle": true, "server": true},
		"url":          {"url": true},
	}
	kindAllowed, known := allowed[contact.Kind]
	if !known {
		return
	}
	for key := range contact.Extensions {
		if !strings.HasPrefix(key, "x-") {
			*errs = append(*errs, fmt.Errorf("%s.%s: unknown key for contact kind %s", path, key, contact.Kind))
		}
	}
	set := map[string]bool{
		"url": contact.URL != "", "skill": contact.Skill != "", "address": contact.Address != "",
		"subject-prefix": contact.SubjectPrefix != "", "repo": contact.Repo != "", "labels": len(contact.Labels) > 0,
		"template": contact.Template != "", "project": contact.Project != "", "issue-type": contact.IssueType != "",
		"channel": contact.Channel != "", "handle": contact.Handle != "", "server": contact.Server != "",
	}
	for key, present := range set {
		if present && !kindAllowed[key] {
			*errs = append(*errs, fmt.Errorf("%s.%s: not valid for contact kind %s", path, key, contact.Kind))
		}
	}
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
	var node yaml.Node
	if err := node.Encode(m); err != nil {
		return nil, err
	}
	return encodeDocument(&node)
}

// UpdateDependencies edits only the dependencies sequence in an existing
// manifest. YAML nodes retain comments, scalar styles, flow collections, and
// extension fields that are not owned by the dependency editor.
func UpdateDependencies(original []byte, dependencies []Dependency) ([]byte, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(original, &document); err != nil {
		return nil, err
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("manifest root must be a mapping")
	}
	root := document.Content[0]
	keyIndex, sequence := mappingEntry(root, "dependencies")
	if sequence == nil {
		if len(dependencies) == 0 {
			return original, nil
		}
		key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "dependencies"}
		sequence = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		root.Content = append(root.Content, key, sequence)
	} else if sequence.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("dependencies must be a sequence")
	}

	desired := make(map[string]Dependency, len(dependencies))
	for _, dependency := range dependencies {
		desired[dependency.ID] = dependency
	}
	seen := map[string]bool{}
	kept := make([]*yaml.Node, 0, len(dependencies))
	for _, item := range sequence.Content {
		id := mappingScalar(item, "id")
		dependency, ok := desired[id]
		if !ok {
			continue
		}
		updateDependencyNode(item, dependency)
		kept = append(kept, item)
		seen[id] = true
	}
	for _, dependency := range dependencies {
		if seen[dependency.ID] {
			continue
		}
		var item yaml.Node
		if err := item.Encode(dependency); err != nil {
			return nil, err
		}
		kept = append(kept, &item)
	}
	sequence.Content = kept
	if len(dependencies) == 0 && keyIndex >= 0 {
		root.Content = append(root.Content[:keyIndex], root.Content[keyIndex+2:]...)
	}
	return encodeDocument(&document)
}

func Format(original []byte) ([]byte, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(original, &document); err != nil {
		return nil, err
	}
	return encodeDocument(&document)
}

func encodeDocument(document *yaml.Node) ([]byte, error) {
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(document); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return append(bytes.TrimRight(out.Bytes(), "\n"), '\n'), nil
}

func mappingEntry(mapping *yaml.Node, key string) (int, *yaml.Node) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return -1, nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return i, mapping.Content[i+1]
		}
	}
	return -1, nil
}

func mappingScalar(mapping *yaml.Node, key string) string {
	_, value := mappingEntry(mapping, key)
	if value == nil {
		return ""
	}
	return value.Value
}

func updateDependencyNode(node *yaml.Node, dependency Dependency) {
	setScalar := func(key, value string, omit bool) {
		index, current := mappingEntry(node, key)
		if omit {
			if index >= 0 {
				node.Content = append(node.Content[:index], node.Content[index+2:]...)
			}
			return
		}
		if current == nil {
			node.Content = append(node.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
			return
		}
		current.Kind = yaml.ScalarNode
		current.Tag = "!!str"
		current.Value = value
	}
	setScalar("id", dependency.ID, dependency.ID == "")
	setScalar("git", dependency.Git, false)
	setScalar("ref", dependency.Ref, dependency.Ref == "")
	setScalar("path", dependency.Path, dependency.Path == "")
	setScalar("track", dependency.Track, dependency.Track == "")
	wireIndex, wireNode := mappingEntry(node, "wire")
	if dependency.Wire == nil {
		if wireIndex >= 0 {
			node.Content = append(node.Content[:wireIndex], node.Content[wireIndex+2:]...)
		}
	} else {
		style := yaml.Style(0)
		if wireNode != nil {
			style = wireNode.Style
		}
		var encoded yaml.Node
		_ = encoded.Encode(*dependency.Wire)
		encoded.Style = style
		if wireNode == nil {
			node.Content = append(node.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "wire"}, &encoded)
		} else {
			*wireNode = encoded
		}
	}
}

func MarshalLock(l *Lock) ([]byte, error) {
	return yaml.Marshal(l)
}
