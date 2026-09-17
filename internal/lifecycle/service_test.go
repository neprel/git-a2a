package lifecycle

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/v2/internal/adapter"
	"github.com/neprel/git-a2a/v2/internal/cardmetadata"
	"github.com/neprel/git-a2a/v2/internal/manifest"
)

type fakeAdapter struct{ fail bool }

func (fakeAdapter) Ecosystem() string                                           { return "golang" }
func (fakeAdapter) Detect(string) (bool, adapter.Variant, error)                { return true, "go", nil }
func (fakeAdapter) Capability(string, adapter.Dependency, adapter.Export) error { return nil }
func (f fakeAdapter) Pull(_ context.Context, root string, _ adapter.Dependency, exp adapter.Export, l adapter.Locked) (adapter.Change, error) {
	if f.fail {
		return adapter.Change{}, fmt.Errorf("injected failure")
	}
	p := filepath.Join(root, "fake.bindings")
	old, _ := os.ReadFile(p)
	line := exp.Name + "=" + l.Commit + "\n"
	if strings.Contains(string(old), line) {
		return adapter.Change{File: "fake.bindings"}, nil
	}
	return adapter.Change{File: "fake.bindings", Changed: true}, os.WriteFile(p, []byte(line), 0o644)
}
func (fakeAdapter) Remove(_ context.Context, root string, _ adapter.Dependency, _ adapter.Export, _ adapter.Locked) (adapter.Change, error) {
	err := os.Remove(filepath.Join(root, "fake.bindings"))
	if os.IsNotExist(err) {
		err = nil
	}
	return adapter.Change{Changed: err == nil}, err
}
func (fakeAdapter) Inspect(_ context.Context, root string, _ adapter.Dependency, exp adapter.Export, locked adapter.Locked) ([]adapter.Finding, error) {
	body, err := os.ReadFile(filepath.Join(root, "fake.bindings"))
	if os.IsNotExist(err) {
		return []adapter.Finding{{File: "fake.bindings", Entry: exp.Name, Want: locked.Commit}}, nil
	}
	if err != nil {
		return nil, err
	}
	want := exp.Name + "=" + locked.Commit
	if !strings.Contains(string(body), want) {
		return []adapter.Finding{{File: "fake.bindings", Entry: exp.Name, Want: want, Got: strings.TrimSpace(string(body))}}, nil
	}
	return nil, nil
}

type controlledAdapter struct {
	variant            adapter.Variant
	wantGit            string
	pullErr, removeErr bool
	finding            *adapter.Finding
	pullCalls, pulls   *int
}

func (a controlledAdapter) Ecosystem() string                            { return "golang" }
func (a controlledAdapter) Detect(string) (bool, adapter.Variant, error) { return true, a.variant, nil }
func (a controlledAdapter) Capability(_ string, dep adapter.Dependency, _ adapter.Export) error {
	if a.wantGit != "" && dep.Git != a.wantGit {
		return fmt.Errorf("capability source = %q, want %q", dep.Git, a.wantGit)
	}
	return nil
}
func (a controlledAdapter) Pull(context.Context, string, adapter.Dependency, adapter.Export, adapter.Locked) (adapter.Change, error) {
	if a.pullCalls != nil {
		*a.pullCalls++
	}
	if a.pullErr {
		return adapter.Change{}, fmt.Errorf("pull failure")
	}
	if a.pulls != nil {
		*a.pulls++
	}
	return adapter.Change{}, nil
}
func (a controlledAdapter) Remove(context.Context, string, adapter.Dependency, adapter.Export, adapter.Locked) (adapter.Change, error) {
	if a.removeErr {
		return adapter.Change{}, fmt.Errorf("remove failure")
	}
	return adapter.Change{Changed: true}, nil
}
func (a controlledAdapter) Inspect(context.Context, string, adapter.Dependency, adapter.Export, adapter.Locked) ([]adapter.Finding, error) {
	if a.finding == nil {
		return nil, nil
	}
	return []adapter.Finding{*a.finding}, nil
}

func TestLifecycleEndToEnd(t *testing.T) {
	remote := newRemote(t, "lib", "surface-v1")
	consumer := t.TempDir()
	s := Service{Root: consumer, Adapters: []adapter.Adapter{fakeAdapter{}}}
	if err := s.Init("consumer", ""); err != nil {
		t.Fatal(err)
	}
	result, err := s.Add(context.Background(), remote, "dep", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	first := result.Commit
	items, err := s.List()
	if err != nil || len(items) != 1 || items[0].Agent == nil || items[0].Agent.Card != "https://agents.example/lib/card.json" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if body, err := os.ReadFile(filepath.Join(consumer, ".git-a2a", "surfaces", "dep", "README.md")); err != nil || string(body) != "surface-v1\n" {
		t.Fatalf("surface=%q err=%v", body, err)
	}
	writeRemote(t, remote, "surface-v2")
	results, err := s.Pull(context.Background(), "dep")
	if err != nil || len(results) != 1 || results[0].Commit == first {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	if _, err = s.Remove(context.Background(), "dep"); err != nil {
		t.Fatal(err)
	}
	items, err = s.List()
	if err != nil || len(items) != 0 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}

func TestPullWithoutDependenciesSkipsAdaptersAndStateWrites(t *testing.T) {
	root := t.TempDir()
	pullCalls := 0
	s := Service{Root: root, Adapters: []adapter.Adapter{controlledAdapter{variant: "go", pullCalls: &pullCalls}}}
	if err := s.Init("consumer", ""); err != nil {
		t.Fatal(err)
	}
	manifestBefore, err := os.ReadFile(filepath.Join(root, manifest.CanonicalName))
	if err != nil {
		t.Fatal(err)
	}
	results, err := s.Pull(context.Background(), "")
	if err != nil || len(results) != 0 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	manifestAfter, err := os.ReadFile(filepath.Join(root, manifest.CanonicalName))
	if err != nil {
		t.Fatal(err)
	}
	if string(manifestAfter) != string(manifestBefore) || pullCalls != 0 {
		t.Fatalf("empty pull mutated state or called adapters (calls=%d)", pullCalls)
	}
	if _, err := os.Stat(filepath.Join(root, "a2amodule.lock")); !os.IsNotExist(err) {
		t.Fatalf("empty pull created lock: %v", err)
	}
	if _, err := s.Pull(context.Background(), "missing"); err == nil || !strings.Contains(err.Error(), `dependency "missing" not found`) {
		t.Fatalf("unknown dependency error=%v", err)
	}
}

func TestRelativeAgentCardLifecycleIsExactOfflineAndRecoverable(t *testing.T) {
	remote := newRemote(t, "lib", "surface")
	cardPath := filepath.Join(remote, "metadata", "owner.card")
	first := []byte("{\"version\":1,\"signature\":\"opaque-a\"}\n")
	if err := os.MkdirAll(filepath.Dir(cardPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cardPath, first, 0o644); err != nil {
		t.Fatal(err)
	}
	writeRelativeCardManifest(t, remote, "metadata/owner.card")
	git(t, remote, "add", ".")
	git(t, remote, "commit", "-m", "relative card")

	consumer := t.TempDir()
	s := Service{Root: consumer, Adapters: []adapter.Adapter{fakeAdapter{}}}
	if err := s.Init("consumer", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(context.Background(), remote, "dep", "main", ""); err != nil {
		t.Fatal(err)
	}
	localCard := filepath.Join(consumer, ".git-a2a", "agents", "dep", "agent-card.json")
	if got, err := os.ReadFile(localCard); err != nil || string(got) != string(first) {
		t.Fatalf("card=%q err=%v", got, err)
	}
	owner, err := s.Whose("dep")
	if err != nil || owner.Agent.DeclaredCard != "metadata/owner.card" || owner.Agent.Card != ".git-a2a/agents/dep/agent-card.json" {
		t.Fatalf("whose=%+v err=%v", owner, err)
	}
	if err = os.Remove(localCard); err != nil {
		t.Fatal(err)
	}
	owner, err = s.Whose("dep")
	if err != nil || owner.Agent.Card != "" || len(owner.Problems) == 0 {
		t.Fatalf("missing-card whose=%+v err=%v", owner, err)
	}
	if _, err = s.Pull(context.Background(), "dep"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(localCard); err != nil || string(got) != string(first) {
		t.Fatalf("restored card=%q err=%v", got, err)
	}

	second := []byte("{\"version\":2,\"signature\":\"opaque-b\"}\n")
	if err = os.WriteFile(cardPath, second, 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, remote, "add", ".")
	git(t, remote, "commit", "-m", "card v2")
	if _, err = s.Pull(context.Background(), "dep"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(localCard); err != nil || string(got) != string(second) {
		t.Fatalf("updated card=%q err=%v", got, err)
	}

	writeRelativeCardManifest(t, remote, "https://agents.example/lib/card.json")
	git(t, remote, "add", "a2amodule.yml")
	git(t, remote, "commit", "-m", "external card")
	if _, err = s.Pull(context.Background(), "dep"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(localCard); !os.IsNotExist(err) {
		t.Fatalf("obsolete card remains: %v", err)
	}
	owner, err = s.Whose("dep")
	if err != nil || owner.Agent.Card != "https://agents.example/lib/card.json" {
		t.Fatalf("https whose=%+v err=%v", owner, err)
	}
}

func writeRelativeCardManifest(t *testing.T, root, card string) {
	t.Helper()
	body := fmt.Sprintf("schema: 2\ncomponent:\n  id: lib\n  exports:\n    - adapter: golang\n      name: example\nagent:\n  name: owner\n  card: %s\n", card)
	if err := os.WriteFile(filepath.Join(root, "a2amodule.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAddFailureDoesNotWriteDeclarationOrLock(t *testing.T) {
	remote := newRemote(t, "lib", "surface")
	root := t.TempDir()
	s := Service{Root: root, Adapters: []adapter.Adapter{fakeAdapter{fail: true}}}
	if err := s.Init("consumer", ""); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, "a2amodule.yml"))
	if _, err := s.Add(context.Background(), remote, "dep", "main", ""); err == nil {
		t.Fatal("failure accepted")
	}
	after, _ := os.ReadFile(filepath.Join(root, "a2amodule.yml"))
	if string(before) != string(after) {
		t.Fatal("manifest changed on failure")
	}
	if _, err := os.Stat(filepath.Join(root, "a2amodule.lock")); !os.IsNotExist(err) {
		t.Fatalf("lock exists: %v", err)
	}
}

func TestAddInvalidAliasFailsBeforeAdapterMutation(t *testing.T) {
	remote := newRemote(t, "lib", "surface")
	root := t.TempDir()
	pullCalls := 0
	s := Service{Root: root, Adapters: []adapter.Adapter{controlledAdapter{variant: "go", pullCalls: &pullCalls}}}
	if err := s.Init("consumer", ""); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, manifest.CanonicalName))
	if _, err := s.Add(context.Background(), remote, "BAD", "main", ""); err == nil || !strings.Contains(err.Error(), "must match") {
		t.Fatalf("invalid alias error = %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(root, manifest.CanonicalName))
	if string(before) != string(after) || pullCalls != 0 {
		t.Fatalf("persistent state changed before validation (pull calls=%d)", pullCalls)
	}
	if _, err := os.Stat(filepath.Join(root, "a2amodule.lock")); !os.IsNotExist(err) {
		t.Fatalf("lock created: %v", err)
	}
}

func TestRemoveManagerFailureKeepsDeclarationLockAndMaterialization(t *testing.T) {
	remote := newRemote(t, "lib", "surface")
	root := t.TempDir()
	s := Service{Root: root, Adapters: []adapter.Adapter{fakeAdapter{}}}
	if err := s.Init("consumer", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(context.Background(), remote, "dep", "main", ""); err != nil {
		t.Fatal(err)
	}
	manifestBefore, _ := os.ReadFile(filepath.Join(root, manifest.CanonicalName))
	lockBefore, _ := os.ReadFile(filepath.Join(root, "a2amodule.lock"))
	s.Adapters = []adapter.Adapter{controlledAdapter{variant: "go", removeErr: true}}
	if _, err := s.Remove(context.Background(), "dep"); err == nil || !strings.Contains(err.Error(), "remove failure") {
		t.Fatalf("remove error = %v", err)
	}
	manifestAfter, _ := os.ReadFile(filepath.Join(root, manifest.CanonicalName))
	lockAfter, _ := os.ReadFile(filepath.Join(root, "a2amodule.lock"))
	if string(manifestBefore) != string(manifestAfter) || string(lockBefore) != string(lockAfter) {
		t.Fatal("git-a2a state changed after native manager failure")
	}
	if _, err := os.Stat(filepath.Join(root, "fake.bindings")); err != nil {
		t.Fatalf("materialization was lost: %v", err)
	}
}

func TestUnsupportedNPMSubdirectorySelectsSubmoduleWithoutPartialNativeBinding(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{\"name\":\"consumer\",\"private\":true}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	upstream := &manifest.Manifest{Component: manifest.Component{Exports: []manifest.Export{{Adapter: "npm", Name: "lib", Path: "packages/lib"}}}}
	bindings, warning, err := (Service{Root: root}).selectBindings(root, "lib", "https://example.test/lib.git", "", upstream)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 || bindings[0].Adapter != "submodule" || warning == "" {
		t.Fatalf("bindings=%+v warning=%q", bindings, warning)
	}
}

func TestAddUnsupportedNPMSubdirectoryUsesOneSubmoduleAndNoNPMEntry(t *testing.T) {
	t.Setenv("GIT_ALLOW_PROTOCOL", "file")
	remote := t.TempDir()
	git(t, remote, "init", "-b", "main")
	git(t, remote, "config", "user.email", "test@example.com")
	git(t, remote, "config", "user.name", "Test")
	mustWrite := func(name, body string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(remote, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(remote, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("a2amodule.yml", "schema: 2\ncomponent:\n  id: lib\n  exports:\n    - adapter: npm\n      name: lib\n      path: packages/lib\nagent:\n  card: https://agents.example/lib.json\n")
	mustWrite("packages/lib/package.json", "{\"name\":\"lib\",\"version\":\"1.0.0\"}\n")
	git(t, remote, "add", ".")
	git(t, remote, "commit", "-m", "fixture")

	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	git(t, root, "config", "user.email", "consumer@example.com")
	git(t, root, "config", "user.name", "Consumer")
	packageJSON := []byte("{\"name\":\"consumer\",\"private\":true}\n")
	if err := os.WriteFile(filepath.Join(root, "package.json"), packageJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "package.json")
	git(t, root, "commit", "-m", "consumer")
	s := Service{Root: root}
	if err := s.Init("consumer", ""); err != nil {
		t.Fatal(err)
	}
	result, err := s.Add(context.Background(), remote, "lib", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Warning == "" {
		t.Fatal("fallback did not report missing automatic native integration")
	}
	after, _ := os.ReadFile(filepath.Join(root, "package.json"))
	if string(after) != string(packageJSON) {
		t.Fatalf("npm manifest changed during fallback: %s", after)
	}
	items, err := s.List()
	if err != nil || len(items[0].Bindings) != 1 || items[0].Bindings[0].Adapter != "submodule" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}

func TestSelectionPreservesGradleAndMavenConsumersForOneMavenExport(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	for name, body := range map[string]string{
		"pom.xml":             "<project/>",
		"settings.gradle.kts": "rootProject.name = \"consumer\"\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	upstream := &manifest.Manifest{Component: manifest.Component{Exports: []manifest.Export{{Adapter: "maven", Name: "org.example:lib"}}}}
	bindings, _, err := (Service{Root: root}).selectBindings(root, "lib", "https://example.test/lib.git", "", upstream)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, binding := range bindings {
		got[binding.Adapter+"/"+binding.Variant] = true
	}
	for _, want := range []string{"submodule/git", "maven/gradle-kts", "maven/maven"} {
		if !got[want] {
			t.Fatalf("missing %s in %+v", want, bindings)
		}
	}
}

func TestFindExportReportsCurrentUpstreamPath(t *testing.T) {
	upstream := &manifest.Manifest{Component: manifest.Component{Exports: []manifest.Export{{Adapter: "npm", Name: "pkg", Path: "packages/new"}}}}
	exp, ok := findExport(upstream, manifest.Binding{Adapter: "npm", Variant: "npm", Export: "pkg", Path: "packages/saved"})
	if !ok || exp.Path != "packages/new" {
		t.Fatalf("export=%+v ok=%v", exp, ok)
	}
}

func TestPullDoesNotSwitchSavedNPMBindingWhenUpstreamBecomesUnsupported(t *testing.T) {
	root := t.TempDir()
	original := []byte("{\"name\":\"consumer\",\"private\":true}\n")
	if err := os.WriteFile(filepath.Join(root, "package.json"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	dep := manifest.Dependency{Name: "lib", Git: "https://example.test/lib.git", Bindings: []manifest.Binding{{Adapter: "npm", Variant: "npm", Export: "lib"}}}
	upstream := &manifest.Manifest{Component: manifest.Component{Exports: []manifest.Export{{Adapter: "npm", Name: "lib", Path: "packages/lib"}}}}
	commit := strings.Repeat("a", 40)
	_, err := (Service{Root: root}).applyTransaction(context.Background(), dep, upstream, adapter.Locked{Git: dep.Git, Commit: commit}, nil, testCard(commit), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "refusing to switch adapter") {
		t.Fatalf("error = %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "package.json"))
	if string(after) != string(original) {
		t.Fatal("npm manifest changed before unsupported-source rejection")
	}
}

func TestMissingNativeToolIsErrorNotSubmoduleFallback(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{\"name\":\"consumer\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	dep := manifest.Dependency{Name: "lib", Bindings: []manifest.Binding{{Adapter: "npm", Variant: "npm", Export: "lib"}}}
	err := (Service{Root: root}).preflightBindings(context.Background(), dep)
	if err == nil || !strings.Contains(err.Error(), "npm not found") {
		t.Fatalf("missing tool error = %v", err)
	}
}

func TestPreflightDriftStopsWire(t *testing.T) {
	pullCalls := 0
	impl := controlledAdapter{variant: "go", pullCalls: &pullCalls, finding: &adapter.Finding{File: "go.mod", Entry: "example", Want: "new", Got: "user value"}}
	s := Service{Root: t.TempDir(), Adapters: []adapter.Adapter{impl}}
	dep := adapter.Dependency{Name: "lib", Bindings: []manifest.Binding{{Adapter: "golang", Variant: "go", Export: "example"}}}
	up := &manifest.Manifest{Component: manifest.Component{Exports: []manifest.Export{{Adapter: "golang", Name: "example"}}}}
	locked := adapter.Locked{Commit: strings.Repeat("a", 40)}
	if _, err := s.applyTransaction(context.Background(), dep, up, locked, nil, testCard(locked.Commit), t.TempDir()); err == nil || !strings.Contains(err.Error(), "user value") {
		t.Fatalf("err=%v", err)
	}
	if pullCalls != 0 {
		t.Fatalf("Pull called %d times", pullCalls)
	}
}

func TestNewBindingRefusesToClaimMatchingUserEntry(t *testing.T) {
	impl := controlledAdapter{variant: "go"}
	s := Service{Root: t.TempDir(), Adapters: []adapter.Adapter{impl}}
	dep := adapter.Dependency{Name: "lib", Bindings: []manifest.Binding{{Adapter: "golang", Variant: "go", Export: "example"}}}
	err := s.preflightNewBindings(context.Background(), dep, adapter.Locked{Commit: strings.Repeat("a", 40)})
	if err == nil || !strings.Contains(err.Error(), "refusing to claim ownership") {
		t.Fatalf("err=%v", err)
	}
}

func TestCommitAdvancePullsUnchangedBinding(t *testing.T) {
	pulls := 0
	impl := controlledAdapter{variant: "go", pulls: &pulls}
	s := Service{Root: t.TempDir(), Adapters: []adapter.Adapter{impl}}
	dep := adapter.Dependency{Name: "lib", Bindings: []manifest.Binding{{Adapter: "golang", Variant: "go", Export: "example"}}}
	up := &manifest.Manifest{Component: manifest.Component{Exports: []manifest.Export{{Adapter: "golang", Name: "example"}}}}
	old := adapter.Locked{Commit: strings.Repeat("a", 40)}
	locked := adapter.Locked{Commit: strings.Repeat("b", 40)}
	tx, err := s.applyTransaction(context.Background(), dep, up, locked, &old, testCard(locked.Commit), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tx.finalize()
	if pulls != 1 {
		t.Fatalf("pulls=%d", pulls)
	}
}

func TestSelectBindingsPassesDependencySourceToCapability(t *testing.T) {
	const source = "https://example.test/lib.git"
	upstream := &manifest.Manifest{Component: manifest.Component{Exports: []manifest.Export{{Adapter: "golang", Name: "example.test/lib"}}}}
	s := Service{Root: t.TempDir(), Adapters: []adapter.Adapter{controlledAdapter{variant: "go", wantGit: source}}}
	bindings, _, err := s.selectBindings(s.Root, "lib", source, "", upstream)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 || bindings[0].Adapter != "golang" {
		t.Fatalf("bindings=%+v", bindings)
	}
}

func TestPreflightAllowsRepairableMaterializationButRejectsDeclarationDrift(t *testing.T) {
	dep := adapter.Dependency{Name: "lib", Bindings: []manifest.Binding{{Adapter: "golang", Variant: "go", Export: "example"}}}
	binding := dep.Bindings[0]
	locked := adapter.Locked{Commit: strings.Repeat("a", 40)}

	repairable := adapter.Finding{File: "manager.lock", Entry: "example", Want: locked.Commit, Got: "different installed revision", Repairable: true}
	s := Service{Root: t.TempDir(), Adapters: []adapter.Adapter{controlledAdapter{variant: "go", finding: &repairable}}}
	if err := s.preflightDrift(context.Background(), dep, binding, locked); err != nil {
		t.Fatalf("repairable materialization blocked Pull: %v", err)
	}

	declaration := adapter.Finding{File: "consumer.manifest", Entry: "example", Want: locked.Commit, Got: "user-edited source"}
	s.Adapters = []adapter.Adapter{controlledAdapter{variant: "go", finding: &declaration}}
	if err := s.preflightDrift(context.Background(), dep, binding, locked); err == nil || !strings.Contains(err.Error(), "user-edited source") {
		t.Fatalf("declaration drift was not rejected: %v", err)
	}
}

func TestRollbackFailureIsReported(t *testing.T) {
	first := controlledAdapter{variant: "go", removeErr: true}
	second := controlledAdapter{variant: "go-alt", pullErr: true}
	s := Service{Root: t.TempDir(), Adapters: []adapter.Adapter{first, second}}
	dep := adapter.Dependency{Name: "lib", Bindings: []manifest.Binding{
		{Adapter: "golang", Variant: "go", Export: "example"},
		{Adapter: "golang", Variant: "go-alt", Export: "example"},
	}}
	up := &manifest.Manifest{Component: manifest.Component{Exports: []manifest.Export{{Adapter: "golang", Name: "example"}}}}
	commit := strings.Repeat("a", 40)
	_, err := s.applyTransaction(context.Background(), dep, up, adapter.Locked{Commit: commit}, nil, testCard(commit), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "rollback failed") || !strings.Contains(err.Error(), "remove failure") {
		t.Fatalf("err=%v", err)
	}
}

func testCard(commit string) cardmetadata.Prepared {
	return cardmetadata.Prepared{Agent: manifest.LockedAgent{Name: "owner", DeclaredCard: "https://agents.example/card.json", Card: "https://agents.example/card.json", Commit: commit}}
}

func TestInitRejectsGitignoreSymlinkWithoutCreatingManifest(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "outside")
	if err := os.WriteFile(target, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, ".gitignore")); err != nil {
		t.Skip(err)
	}
	if err := (Service{Root: root}).Init("consumer", ""); err == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := os.Stat(filepath.Join(root, manifest.CanonicalName)); !os.IsNotExist(err) {
		t.Fatalf("manifest exists: %v", err)
	}
	if body, err := os.ReadFile(target); err != nil || string(body) != "keep\n" {
		t.Fatalf("target=%q err=%v", body, err)
	}
}

func newRemote(t *testing.T, id, surface string) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	git(t, root, "config", "user.email", "test@example.com")
	git(t, root, "config", "user.name", "Test")
	writeManifestFixture(t, root, id, surface)
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial")
	return root
}
func writeRemote(t *testing.T, root, surface string) {
	t.Helper()
	writeManifestFixture(t, root, "lib", surface)
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "advance")
}
func writeManifestFixture(t *testing.T, root, id, surface string) {
	t.Helper()
	body := fmt.Sprintf("schema: 2\ncomponent:\n  id: %s\n  exports:\n    - adapter: golang\n      name: example\n  surface: public\nagent:\n  name: owner\n  card: https://agents.example/%s/card.json\n", id, id)
	if err := os.WriteFile(filepath.Join(root, "a2amodule.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "public", "README.md"), []byte(surface+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}
