package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	contacttemplate "github.com/neprel/git-a2a/internal/contact/template"
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
			for j, source := range a.Trust.JWKS {
				parsed, err := url.Parse(source)
				if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
					errs = append(errs, fmt.Errorf("%s.trust.jwks[%d]: must be an https URL", p, j))
				}
			}
			for j, key := range a.Trust.Keys {
				if strings.TrimSpace(key) == "" {
					errs = append(errs, fmt.Errorf("%s.trust.keys[%d]: must not be empty", p, j))
				}
			}
			for j, origin := range a.Trust.Origins {
				parsed, err := url.Parse(origin)
				if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
					errs = append(errs, fmt.Errorf("%s.trust.origins[%d]: must be scheme://host[:port]", p, j))
				}
			}
			if a.Trust.JWKSMaxAge != "" {
				if duration, err := time.ParseDuration(a.Trust.JWKSMaxAge); err != nil || duration <= 0 {
					errs = append(errs, fmt.Errorf("%s.trust.jwks-max-age: must be a positive duration", p))
				}
			}
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
	if m.Settings != nil {
		validateExtensions("settings", m.Settings.Extensions, &errs)
		validateVendorPath("settings.vendor-dir", m.Settings.VendorDir, &errs)
		for i, target := range m.Settings.SyncTargets {
			validateRelative(fmt.Sprintf("settings.sync-targets[%d]", i), target, &errs)
		}
		for i, organisation := range m.Settings.Organisation {
			if strings.TrimSpace(organisation) == "" {
				errs = append(errs, fmt.Errorf("settings.organisation[%d]: must not be empty", i))
			}
		}
		if m.Settings.Contact != nil {
			validateExtensions("settings.contact", m.Settings.Contact.Extensions, &errs)
			for i, origin := range m.Settings.Contact.AllowHTTP {
				parsed, err := url.Parse(origin)
				if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
					errs = append(errs, fmt.Errorf("settings.contact.allow-http[%d]: must be an https origin", i))
				}
			}
			for i, name := range m.Settings.Contact.AllowExec {
				if strings.TrimSpace(name) == "" || strings.ContainsAny(name, `/\\`) {
					errs = append(errs, fmt.Errorf("settings.contact.allow-exec[%d]: must be a bare binary name", i))
				}
			}
		}
	}
	seen := map[string]bool{}
	vendorPaths := map[string]string{}
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
		if d.Vendor != nil {
			if d.ID == "" {
				errs = append(errs, fmt.Errorf("%s.id: required when vendor is set", p))
			}
			mode := d.Vendor.Mode
			if mode == "" {
				mode = "submodule"
			}
			if mode != "submodule" && mode != "copy" {
				errs = append(errs, fmt.Errorf("%s.vendor.mode: must be submodule or copy", p))
			}
			if mode == "copy" && d.Vendor.Recursive {
				errs = append(errs, fmt.Errorf("%s.vendor.recursive: copy mode cannot initialise nested submodules", p))
			}
			validateVendorPath(p+".vendor.path", d.Vendor.Path, &errs)
			validateExtensions(p+".vendor", d.Vendor.Extensions, &errs)
			resolved := resolvedVendorPath(m, d)
			validateVendorPath(p+".vendor.path", resolved, &errs)
			if overlapsPath(resolved, m.Module.Surface) {
				errs = append(errs, fmt.Errorf("%s.vendor.path: must not overlap module.surface", p))
			}
			for other, otherID := range vendorPaths {
				if overlapsPath(resolved, other) {
					errs = append(errs, fmt.Errorf("%s.vendor.path: overlaps dependency %s vendor path %s", p, otherID, other))
				}
			}
			vendorPaths[resolved] = d.ID
		}
		if d.Require != nil {
			if d.Require.Commits != "" && d.Require.Commits != "any" && d.Require.Commits != "signed" {
				errs = append(errs, fmt.Errorf("%s.require.commits: must be any or signed", p))
			}
			if d.Require.Commits == "signed" && d.Require.Signers == "" {
				errs = append(errs, fmt.Errorf("%s.require.signers: required when commits is signed", p))
			}
			validateRelative(p+".require.signers", d.Require.Signers, &errs)
			if d.Require.Cards != "" && d.Require.Cards != "any" && d.Require.Cards != "signed" {
				errs = append(errs, fmt.Errorf("%s.require.cards: must be any or signed", p))
			}
			validateExtensions(p+".require", d.Require.Extensions, &errs)
		}
		validateExtensions(p, d.Extensions, &errs)
	}
	return errors.Join(errs...)
}

func validateContact(path string, contact Contact, errs *[]error) {
	allowed := map[string]map[string]bool{
		"a2a":             {"url": true, "skill": true},
		"email":           {"address": true, "subject-prefix": true},
		"github-issue":    {"repo": true, "server": true, "labels": true, "template": true},
		"gitlab-issue":    {"repo": true, "server": true, "labels": true, "template": true},
		"gitea-issue":     {"repo": true, "server": true, "labels": true},
		"bitbucket-issue": {"repo": true, "labels": true},
		"azure-boards":    {"organization": true, "project": true, "issue-type": true},
		"http":            {"url": true, "method": true, "headers": true, "content-type": true, "body": true},
		"exec":            {"command": true, "args": true, "stdin": true},
		"jira":            {"url": true, "project": true, "issue-type": true},
		"mattermost":      {"channel": true, "handle": true, "server": true},
		"slack":           {"channel": true, "handle": true, "server": true},
		"discord":         {"channel": true, "handle": true, "server": true},
		"telegram":        {"channel": true, "handle": true, "server": true},
		"teams":           {"channel": true, "handle": true, "server": true},
		"url":             {"url": true},
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
		"organization": contact.Organization != "", "channel": contact.Channel != "", "handle": contact.Handle != "", "server": contact.Server != "",
		"method": contact.Method != "", "headers": len(contact.Headers) > 0, "content-type": contact.ContentType != "", "body": contact.Body != "",
		"command": len(contact.Command) > 0, "args": len(contact.Args) > 0, "stdin": contact.Stdin != "",
	}
	for key, present := range set {
		if present && !kindAllowed[key] {
			*errs = append(*errs, fmt.Errorf("%s.%s: not valid for contact kind %s", path, key, contact.Kind))
		}
	}
	validateContactRequirements(path, contact, errs)
}

func validateContactRequirements(path string, contact Contact, errs *[]error) {
	switch contact.Kind {
	case "github-issue", "gitlab-issue", "bitbucket-issue":
		if strings.TrimSpace(contact.Repo) == "" {
			*errs = append(*errs, fmt.Errorf("%s.repo: required for contact kind %s", path, contact.Kind))
		}
	case "gitea-issue":
		if strings.TrimSpace(contact.Repo) == "" {
			*errs = append(*errs, fmt.Errorf("%s.repo: required for contact kind gitea-issue", path))
		}
		if contact.Server == "" {
			*errs = append(*errs, fmt.Errorf("%s.server: required for contact kind gitea-issue", path))
		}
	case "azure-boards":
		if contact.Organization == "" || contact.Project == "" {
			*errs = append(*errs, fmt.Errorf("%s: organization and project are required for contact kind azure-boards", path))
		}
	case "http":
		parsed, err := url.Parse(contact.URL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			*errs = append(*errs, fmt.Errorf("%s.url: must be an https URL for contact kind http", path))
		}
		if parsed != nil && (containsPlaceholder(parsed.Scheme) || containsPlaceholder(parsed.Host) || containsPlaceholder(parsed.Path) || containsPlaceholder(parsed.Fragment)) {
			*errs = append(*errs, fmt.Errorf("%s.url: placeholders are allowed only in query values", path))
		}
		for name, value := range contact.Headers {
			if containsPlaceholder(value) {
				*errs = append(*errs, fmt.Errorf("%s.headers.%s: placeholders are not allowed", path, name))
			}
		}
		validateTemplate(path+".url", contact.URL, false, errs)
		validateTemplate(path+".body", contact.Body, true, errs)
	case "exec":
		if len(contact.Command) == 0 || strings.TrimSpace(contact.Command[0]) == "" {
			*errs = append(*errs, fmt.Errorf("%s.command: required for contact kind exec", path))
		} else if strings.ContainsAny(contact.Command[0], `/\\`) {
			*errs = append(*errs, fmt.Errorf("%s.command[0]: must be a bare binary name", path))
		}
		for i, value := range contact.Command {
			if containsPlaceholder(value) {
				*errs = append(*errs, fmt.Errorf("%s.command[%d]: placeholders are not allowed", path, i))
			}
		}
		for i, value := range contact.Args {
			validateTemplate(fmt.Sprintf("%s.args[%d]", path, i), value, false, errs)
		}
		validateTemplate(path+".stdin", contact.Stdin, true, errs)
	}
}

func containsPlaceholder(value string) bool {
	return len(contacttemplate.Names(value)) > 0
}

func validateTemplate(path, value string, allowMessage bool, errs *[]error) {
	allowed := map[string]string{"intent": "", "module": "", "origin": ""}
	if allowMessage {
		allowed["message"] = ""
	}
	if _, err := contacttemplate.Expand(value, allowed); err != nil {
		*errs = append(*errs, fmt.Errorf("%s: %v", path, err))
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
		for name, key := range d.CardsKeys {
			kp := fmt.Sprintf("%s.cards-keys.%s", p, name)
			if key.KeyID == "" {
				errs = append(errs, fmt.Errorf("%s.kid: required", kp))
			}
			if key.Thumbprint == "" {
				errs = append(errs, fmt.Errorf("%s.thumbprint: required", kp))
			}
			if !commitPattern.MatchString(key.FirstSeen) {
				errs = append(errs, fmt.Errorf("%s.first-seen: must be 40 lowercase hex characters", kp))
			}
			validateExtensions(kp, key.Extensions, &errs)
		}
		if d.Verified != "" && d.Verified != "signed" && d.Verified != "skipped" {
			errs = append(errs, fmt.Errorf("%s.verified: must be signed or skipped", p))
		}
		if d.Surface != "" && !treePattern.MatchString(d.Surface) {
			errs = append(errs, fmt.Errorf("%s.surface: invalid tree id", p))
		}
		if d.Vendor != nil {
			if d.Vendor.Mode != "submodule" && d.Vendor.Mode != "copy" {
				errs = append(errs, fmt.Errorf("%s.vendor.mode: must be submodule or copy", p))
			}
			validateVendorPath(p+".vendor.path", d.Vendor.Path, &errs)
			if d.Vendor.Path == "" {
				errs = append(errs, fmt.Errorf("%s.vendor.path: required", p))
			}
			if d.Vendor.Mode == "copy" && !treePattern.MatchString(d.Vendor.Tree) {
				errs = append(errs, fmt.Errorf("%s.vendor.tree: copy mode requires a tree id", p))
			}
			if d.Vendor.Mode == "submodule" && d.Vendor.Tree != "" {
				errs = append(errs, fmt.Errorf("%s.vendor.tree: only copy mode records a tree id", p))
			}
			validateExtensions(p+".vendor", d.Vendor.Extensions, &errs)
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

func validateVendorPath(field, value string, errs *[]error) {
	if value == "" {
		return
	}
	validateRelative(field, value, errs)
	normal := strings.TrimPrefix(strings.ReplaceAll(filepath.Clean(value), `\`, "/"), "./")
	if normal == ".git" || strings.HasPrefix(normal, ".git/") || normal == ".git-a2a" || strings.HasPrefix(normal, ".git-a2a/") {
		*errs = append(*errs, fmt.Errorf("%s: must not be inside .git or .git-a2a", field))
	}
}

func resolvedVendorPath(m *Manifest, d Dependency) string {
	if d.Vendor != nil && d.Vendor.Path != "" {
		return filepath.ToSlash(filepath.Clean(d.Vendor.Path))
	}
	dir := "deps"
	if m.Settings != nil && m.Settings.VendorDir != "" {
		dir = m.Settings.VendorDir
	}
	return filepath.ToSlash(filepath.Join(dir, d.ID))
}

func overlapsPath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	left = strings.Trim(filepath.ToSlash(filepath.Clean(left)), "/")
	right = strings.Trim(filepath.ToSlash(filepath.Clean(right)), "/")
	return left == right || strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/")
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

// AppendAgent edits only the agents sequence. Existing YAML nodes are retained so their
// comments, ordering, scalar spelling, and flow/block collection styles remain owned by the
// author rather than by the Go encoder.
func AppendAgent(original []byte, agent Agent) ([]byte, error) {
	return editDocument(original, func(root *yaml.Node) error {
		sequence, err := ensureSequence(root, "agents")
		if err != nil {
			return err
		}
		var item yaml.Node
		if err := item.Encode(agent); err != nil {
			return err
		}
		sequence.Content = append(sequence.Content, &item)
		return nil
	})
}

// RemoveAgent removes one matching agents item without rebuilding the remaining sequence.
func RemoveAgent(original []byte, name string) ([]byte, error) {
	return editDocument(original, func(root *yaml.Node) error {
		_, sequence := mappingEntry(root, "agents")
		if sequence == nil || sequence.Kind != yaml.SequenceNode {
			return fmt.Errorf("agents must be a sequence")
		}
		kept := sequence.Content[:0]
		for _, item := range sequence.Content {
			if mappingScalar(item, "name") != name {
				kept = append(kept, item)
			}
		}
		sequence.Content = kept
		return nil
	})
}

// AppendExport edits only module.exports and retains every pre-existing module node.
func AppendExport(original []byte, export Export) ([]byte, error) {
	return editDocument(original, func(root *yaml.Node) error {
		_, module := mappingEntry(root, "module")
		if module == nil || module.Kind != yaml.MappingNode {
			return fmt.Errorf("module must be a mapping")
		}
		sequence, err := ensureSequence(module, "exports")
		if err != nil {
			return err
		}
		var item yaml.Node
		if err := item.Encode(export); err != nil {
			return err
		}
		sequence.Content = append(sequence.Content, &item)
		return nil
	})
}

// UpdatePolicy changes only the explicitly supplied policy fields. Nil pointers mean leave the
// field untouched; non-nil slices replace that consumer vocabulary list, including with empty.
func UpdatePolicy(original []byte, intents [][2]string, may, mayNot *[]string, notes *string) ([]byte, error) {
	return editDocument(original, func(root *yaml.Node) error {
		policy, err := ensureMapping(root, "policy")
		if err != nil {
			return err
		}
		if len(intents) > 0 {
			intentNode, err := ensureMapping(policy, "intents")
			if err != nil {
				return err
			}
			for _, pair := range intents {
				setScalarNode(intentNode, pair[0], pair[1])
			}
		}
		if may != nil || mayNot != nil {
			consumers, err := ensureMapping(policy, "consumers")
			if err != nil {
				return err
			}
			if may != nil {
				setEncodedNode(consumers, "may", *may)
			}
			if mayNot != nil {
				setEncodedNode(consumers, "may-not", *mayNot)
			}
		}
		if notes != nil {
			setScalarNode(policy, "notes", *notes)
		}
		return nil
	})
}

func editDocument(original []byte, edit func(*yaml.Node) error) ([]byte, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(original, &document); err != nil {
		return nil, err
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("manifest root must be a mapping")
	}
	if err := edit(document.Content[0]); err != nil {
		return nil, err
	}
	return encodeDocument(&document)
}

func ensureMapping(parent *yaml.Node, key string) (*yaml.Node, error) {
	_, value := mappingEntry(parent, key)
	if value == nil {
		value = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		parent.Content = append(parent.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
	}
	if value.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s must be a mapping", key)
	}
	return value, nil
}

func ensureSequence(parent *yaml.Node, key string) (*yaml.Node, error) {
	_, value := mappingEntry(parent, key)
	if value == nil {
		value = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		parent.Content = append(parent.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
	}
	if value.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("%s must be a sequence", key)
	}
	return value, nil
}

func setScalarNode(mapping *yaml.Node, key, value string) {
	_, current := mappingEntry(mapping, key)
	if current == nil {
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
		return
	}
	current.Kind, current.Tag, current.Value = yaml.ScalarNode, "!!str", value
}

func setEncodedNode(mapping *yaml.Node, key string, value any) {
	_, current := mappingEntry(mapping, key)
	var encoded yaml.Node
	_ = encoded.Encode(value)
	if current == nil {
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, &encoded)
		return
	}
	style := current.Style
	*current = encoded
	current.Style = style
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
			replaceNodePreservingPresentation(wireNode, encoded)
		}
	}
	vendorIndex, vendorNode := mappingEntry(node, "vendor")
	if dependency.Vendor == nil {
		if vendorIndex >= 0 {
			node.Content = append(node.Content[:vendorIndex], node.Content[vendorIndex+2:]...)
		}
	} else {
		style := yaml.Style(0)
		if vendorNode != nil {
			style = vendorNode.Style
		}
		var encoded yaml.Node
		_ = encoded.Encode(dependency.Vendor)
		encoded.Style = style
		if vendorNode == nil {
			node.Content = append(node.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "vendor"}, &encoded)
		} else {
			replaceNodePreservingPresentation(vendorNode, encoded)
		}
	}
}

func replaceNodePreservingPresentation(current *yaml.Node, encoded yaml.Node) {
	style := current.Style
	head, line, foot := current.HeadComment, current.LineComment, current.FootComment
	*current = encoded
	current.Style = style
	current.HeadComment, current.LineComment, current.FootComment = head, line, foot
}

func MarshalLock(l *Lock) ([]byte, error) {
	return yaml.Marshal(l)
}
