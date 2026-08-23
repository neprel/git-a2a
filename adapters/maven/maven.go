package maven

import (
	"context"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/neprel/git-a2a/internal/adapter"
)

const (
	rootFile      = "pom.xml"
	generatedFile = "deps/git-a2a.maven/pom.xml"
	genHeader     = "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<project xmlns=\"http://maven.apache.org/POM/4.0.0\">\n  <modelVersion>4.0.0</modelVersion>\n  <groupId>git-a2a.generated</groupId>\n  <artifactId>vendored-modules</artifactId>\n  <version>1</version>\n  <packaging>pom</packaging>\n  <modules>\n"
	genFooter     = "  </modules>\n</project>\n"
)

type Adapter struct{}

func (Adapter) Ecosystem() string { return "maven" }
func (Adapter) Detect(root string) (bool, adapter.Variant, error) {
	_, e := os.Stat(filepath.Join(root, rootFile))
	if os.IsNotExist(e) {
		return false, "", nil
	}
	return e == nil, "maven", e
}

func (a Adapter) Wire(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) (adapter.Change, error) {
	if dep.Vendor == nil || locked.Vendor == nil {
		return adapter.Change{}, adapter.NotWirable("Maven reactor integration requires an explicitly vendored dependency")
	}
	coordinate, err := parseCoordinate(exp.Name)
	if err != nil {
		return adapter.Change{}, err
	}
	module := modulePath(dep, exp, locked)
	gp := filepath.Join(root, filepath.FromSlash(generatedFile))
	before, err := read(gp)
	if err != nil {
		return adapter.Change{}, err
	}
	modules, discarded := parseGenerated(before)
	modules[dep.ID] = module
	next := renderGenerated(modules)
	rp := filepath.Join(root, rootFile)
	rb, err := os.ReadFile(rp)
	if err != nil {
		return adapter.Change{}, err
	}
	ra, err := upsertRoot(rb, dep.ID, coordinate)
	if err != nil {
		return adapter.Change{}, err
	}
	changed := string(before) != string(next) || string(rb) != string(ra)
	if !changed {
		return adapter.Change{File: generatedFile, Entry: dep.ID}, nil
	}
	if err = os.MkdirAll(filepath.Dir(gp), 0o755); err == nil {
		err = os.WriteFile(gp, next, 0o644)
	}
	if err == nil {
		err = os.WriteFile(rp, ra, 0o644)
	}
	w := ""
	if discarded {
		w = generatedFile + " contained foreign content; git-a2a regenerated the owned file and discarded it"
	}
	return adapter.Change{File: generatedFile, Entry: dep.ID, Changed: true, Warning: w}, err
}
func (Adapter) Unwire(_ context.Context, root string, dep adapter.Dependency, _ adapter.Export) (adapter.Change, error) {
	gp := filepath.Join(root, filepath.FromSlash(generatedFile))
	before, err := read(gp)
	if err != nil {
		return adapter.Change{}, err
	}
	modules, discarded := parseGenerated(before)
	_, had := modules[dep.ID]
	delete(modules, dep.ID)
	if len(modules) == 0 {
		if err = os.Remove(gp); err != nil && !os.IsNotExist(err) {
			return adapter.Change{}, err
		}
	} else {
		err = os.WriteFile(gp, renderGenerated(modules), 0o644)
	}
	rp := filepath.Join(root, rootFile)
	rb, re := os.ReadFile(rp)
	if re != nil {
		return adapter.Change{}, re
	}
	ra := removeRoot(rb, dep.ID, len(modules) == 0)
	if err == nil && string(rb) != string(ra) {
		err = os.WriteFile(rp, ra, 0o644)
	}
	return adapter.Change{File: generatedFile, Entry: dep.ID, Changed: had || discarded || len(before) > 0 || string(rb) != string(ra)}, err
}
func (a Adapter) Refresh(ctx context.Context, root string, _ adapter.Dependency, _ adapter.Export, _ adapter.Locked) error {
	if err := adapter.RequireTool(ctx, a.Ecosystem(), "maven"); err != nil {
		return err
	}
	return adapter.Command(ctx, root, "mvn", "-B", "package", "-DskipTests")
}
func (Adapter) Drift(_ context.Context, root string, dep adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	if dep.Vendor == nil || locked.Vendor == nil {
		return []adapter.Finding{{File: generatedFile, Entry: dep.ID, Want: "vendored Maven integration", Got: "not vendored"}}, nil
	}
	coord, err := parseCoordinate(exp.Name)
	if err != nil {
		return nil, err
	}
	body, err := read(filepath.Join(root, filepath.FromSlash(generatedFile)))
	if err != nil {
		return nil, err
	}
	mods, foreign := parseGenerated(body)
	var f []adapter.Finding
	if mods[dep.ID] != modulePath(dep, exp, locked) {
		f = append(f, adapter.Finding{File: generatedFile, Entry: dep.ID, Want: modulePath(dep, exp, locked), Got: mods[dep.ID]})
	}
	if foreign {
		f = append(f, adapter.Finding{File: generatedFile, Entry: "owned file", Want: "only generated git-a2a content", Got: "foreign content"})
	}
	rb, err := os.ReadFile(filepath.Join(root, rootFile))
	if err != nil {
		return nil, err
	}
	if !strings.Contains(string(rb), dependencyBlock(dep.ID, coord)) {
		f = append(f, adapter.Finding{File: rootFile, Entry: dep.ID, Want: "managed Maven dependency", Got: "missing or changed"})
	}
	if !strings.Contains(string(rb), moduleBlock()) {
		f = append(f, adapter.Finding{File: rootFile, Entry: "git-a2a reactor", Want: "managed module", Got: "missing"})
	}
	return f, nil
}

func parseCoordinate(v string) ([2]string, error) {
	p := strings.Split(v, ":")
	if len(p) != 2 || p[0] == "" || p[1] == "" {
		return [2]string{}, fmt.Errorf("maven export name %q must be group:artifact", v)
	}
	return [2]string{p[0], p[1]}, nil
}
func modulePath(dep adapter.Dependency, exp adapter.Export, l adapter.Locked) string {
	p := []string{l.Vendor.Path}
	if l.Vendor.Mode == "submodule" && l.Path != "" && l.Path != "." {
		p = append(p, l.Path)
	}
	if exp.Path != "" && exp.Path != "." {
		p = append(p, exp.Path)
	}
	x := filepath.Join(p...)
	if filepath.Base(x) == "pom.xml" {
		x = filepath.Dir(x)
	}
	rel, _ := filepath.Rel("deps/git-a2a.maven", x)
	return filepath.ToSlash(rel)
}
func read(p string) ([]byte, error) {
	b, e := os.ReadFile(p)
	if os.IsNotExist(e) {
		return nil, nil
	}
	return b, e
}

var genBlock = regexp.MustCompile(`(?m)^    <!-- git-a2a: ([a-z0-9][a-z0-9._-]*) -->\n    <module>([^<]+)</module>\n`)

func parseGenerated(b []byte) (map[string]string, bool) {
	m := map[string]string{}
	if len(b) == 0 {
		return m, false
	}
	s := string(b)
	frame := strings.HasPrefix(s, genHeader) && strings.HasSuffix(s, genFooter)
	r := strings.TrimSuffix(strings.TrimPrefix(s, genHeader), genFooter)
	for _, x := range genBlock.FindAllStringSubmatch(s, -1) {
		m[x[1]] = html.UnescapeString(x[2])
		r = strings.Replace(r, x[0], "", 1)
	}
	return m, !frame || strings.TrimSpace(r) != ""
}
func renderGenerated(m map[string]string) []byte {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	b.WriteString(genHeader)
	for _, id := range ids {
		fmt.Fprintf(&b, "    <!-- git-a2a: %s -->\n    <module>%s</module>\n", id, html.EscapeString(m[id]))
	}
	b.WriteString(genFooter)
	return []byte(b.String())
}

func moduleBlock() string {
	return "    <!-- git-a2a:begin reactor -->\n    <module>deps/git-a2a.maven</module>\n    <!-- git-a2a:end reactor -->\n"
}
func dependencyBlock(id string, c [2]string) string {
	return fmt.Sprintf("    <!-- git-a2a:begin dependency %s -->\n    <dependency>\n      <groupId>%s</groupId>\n      <artifactId>%s</artifactId>\n      <version>${project.version}</version>\n    </dependency>\n    <!-- git-a2a:end dependency %s -->\n", id, html.EscapeString(c[0]), html.EscapeString(c[1]), id)
}
func upsertRoot(b []byte, id string, c [2]string) ([]byte, error) {
	s := string(b)
	var err error
	s, err = upsertContainer(s, "modules", moduleBlock())
	if err != nil {
		return nil, err
	}
	s, err = upsertContainer(s, "dependencies", dependencyBlock(id, c))
	return []byte(s), err
}
func upsertContainer(s, name, block string) (string, error) {
	if strings.Contains(s, block) {
		return s, nil
	}
	close := "</" + name + ">"
	if i := strings.Index(s, close); i >= 0 {
		return s[:i] + block + s[i:], nil
	}
	project := strings.LastIndex(s, "</project>")
	if project < 0 {
		return "", fmt.Errorf("pom.xml has no closing </project>")
	}
	owned := fmt.Sprintf("  <!-- git-a2a:begin %s-container -->\n  <%s>\n%s  </%s>\n  <!-- git-a2a:end %s-container -->\n", name, name, block, name, name)
	return s[:project] + owned + s[project:], nil
}
func removeRoot(b []byte, id string, removeReactor bool) []byte {
	s := string(b)
	re := regexp.MustCompile(`(?ms)^    <!-- git-a2a:begin dependency ` + regexp.QuoteMeta(id) + ` -->\n.*?^    <!-- git-a2a:end dependency ` + regexp.QuoteMeta(id) + ` -->\n?`)
	s = re.ReplaceAllString(s, "")
	s = removeEmptyOwnedContainer(s, "dependencies")
	if removeReactor {
		s = strings.Replace(s, moduleBlock(), "", 1)
		s = removeEmptyOwnedContainer(s, "modules")
	}
	return []byte(s)
}
func removeEmptyOwnedContainer(s, name string) string {
	re := regexp.MustCompile(`(?ms)^  <!-- git-a2a:begin ` + name + `-container -->\n  <` + name + `>\n\s*  </` + name + `>\n  <!-- git-a2a:end ` + name + `-container -->\n?`)
	return re.ReplaceAllString(s, "")
}
