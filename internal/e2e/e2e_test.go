package e2e

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/internal/cli"
	"github.com/neprel/git-a2a/internal/gitx"
	"github.com/neprel/git-a2a/internal/manifest"
)

func TestAddUpdateCheckRemoveAgainstLocalBareRepository(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	bare := filepath.Join(tmp, "library.git")
	consumer := filepath.Join(tmp, "consumer")
	mustMkdir(t, source)
	git(t, source, "init", "-b", "main")
	git(t, source, "config", "user.email", "test@example.com")
	git(t, source, "config", "user.name", "Test")
	manifestBytes := []byte("schema: 1\nmodule:\n  id: acme-lib-utils\n  release:\n    channel: main\n")
	mustWrite(t, filepath.Join(source, "a2amodule.yml"), manifestBytes)
	git(t, source, "add", "a2amodule.yml")
	git(t, source, "commit", "-m", "fixture")
	git(t, tmp, "clone", "--bare", source, bare)
	git(t, bare, "config", "uploadpack.allowFilter", "true")
	git(t, bare, "config", "uploadpack.allowAnySHA1InWant", "true")
	mustMkdir(t, consumer)
	mustWrite(t, filepath.Join(consumer, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: acme-app-cli\n"))
	var out, errOut bytes.Buffer
	app := cli.New(&out, &errOut)
	app.Root = consumer
	url := "file://" + bare
	if code := app.Run([]string{"add", url}); code != 0 {
		t.Fatalf("add exit %d: %s", code, errOut.String())
	}
	if !strings.HasPrefix(errOut.String(), "added acme-lib-utils at ") || !strings.Contains(errOut.String(), "using declared release channel main\n") {
		t.Fatalf("unexpected stderr: %q", errOut.String())
	}
	lock, err := manifest.LoadLock(filepath.Join(consumer, "a2amodule.lock"))
	if err != nil {
		t.Fatal(err)
	}
	old := lock.Dependencies["acme-lib-utils"].Commit
	if len(old) != 40 {
		t.Fatalf("bad commit %q", old)
	}
	if _, err := os.Stat(filepath.Join(consumer, ".git-a2a", "cache", "acme-lib-utils", "a2amodule.yml")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"update", "--check"}); code != 0 {
		t.Fatalf("clean check exit %d: %s", code, errOut.String())
	}
	mustWrite(t, filepath.Join(source, "a2amodule.yml"), append(manifestBytes, []byte("x-revision: two\n")...))
	git(t, source, "add", "a2amodule.yml")
	git(t, source, "commit", "-m", "update")
	git(t, source, "push", bare, "main")
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"update", "--check"}); code != 1 {
		t.Fatalf("changed check exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "acme-lib-utils:") {
		t.Fatalf("change did not name dependency: %q", out.String())
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"update", "--review"}); code != 0 {
		t.Fatalf("update exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "--- acme-lib-utils manifest (locked)") || !strings.Contains(out.String(), "+x-revision: two") {
		t.Fatalf("review diff missing:\n%s", out.String())
	}
	next, _ := manifest.LoadLock(filepath.Join(consumer, "a2amodule.lock"))
	if next.Dependencies["acme-lib-utils"].Commit == old {
		t.Fatal("lock did not advance")
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"remove", "acme-lib-utils"}); code != 0 {
		t.Fatalf("remove exit %d: %s", code, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(consumer, ".git-a2a", "cache", "acme-lib-utils")); !os.IsNotExist(err) {
		t.Fatalf("cache still exists: %v", err)
	}
}

func TestAddStoresRemoteDefaultBranchInsteadOfHEAD(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	bare := filepath.Join(tmp, "library.git")
	consumer := filepath.Join(tmp, "consumer")
	mustMkdir(t, source)
	git(t, source, "init", "-b", "trunk")
	git(t, source, "config", "user.email", "test@example.com")
	git(t, source, "config", "user.name", "Test")
	mustWrite(t, filepath.Join(source, "a2amodule.yml"), []byte("schema: 1\nmodule: {id: acme-lib}\n"))
	git(t, source, "add", "a2amodule.yml")
	git(t, source, "commit", "-m", "fixture")
	git(t, tmp, "clone", "--bare", source, bare)
	mustMkdir(t, consumer)
	mustWrite(t, filepath.Join(consumer, "a2amodule.yml"), []byte("schema: 1\nmodule: {id: consumer}\n"))
	var out, errOut bytes.Buffer
	app := cli.New(&out, &errOut)
	app.Root = consumer
	if code := app.Run([]string{"add", "file://" + bare, "--no-wire"}); code != 0 {
		t.Fatalf("add exit %d: %s", code, errOut.String())
	}
	own, err := manifest.Load(filepath.Join(consumer, "a2amodule.yml"))
	if err != nil || len(own.Dependencies) != 1 || own.Dependencies[0].Ref != "trunk" {
		t.Fatalf("dependency ref = %#v, err=%v", own.Dependencies, err)
	}
}

func TestOnlineStatusReusesSparseSourceCheckout(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	bare := filepath.Join(tmp, "library.git")
	consumer := filepath.Join(tmp, "consumer")
	mustMkdir(t, source)
	git(t, source, "init", "-b", "main")
	git(t, source, "config", "user.email", "test@example.com")
	git(t, source, "config", "user.name", "Test")
	mustWrite(t, filepath.Join(source, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: acme-lib\n  release: {channel: main}\n"))
	git(t, source, "add", "a2amodule.yml")
	git(t, source, "commit", "-m", "fixture")
	git(t, tmp, "clone", "--bare", source, bare)
	git(t, bare, "config", "uploadpack.allowFilter", "true")
	git(t, bare, "config", "uploadpack.allowAnySHA1InWant", "true")
	mustMkdir(t, consumer)
	mustWrite(t, filepath.Join(consumer, "a2amodule.yml"), []byte("schema: 1\nmodule: {id: consumer}\n"))
	var out, errOut bytes.Buffer
	runner := &archiveFailCountingRunner{delegate: gitx.ExecRunner{}}
	app := cli.New(&out, &errOut)
	app.Root = consumer
	app.Runner = runner
	if code := app.Run([]string{"add", "file://" + bare, "--no-wire"}); code != 0 {
		t.Fatalf("add exit %d: %s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"sync"}); code != 0 {
		t.Fatalf("sync exit %d: %s", code, errOut.String())
	}
	runner.clones = 0
	for i := 0; i < 2; i++ {
		out.Reset()
		errOut.Reset()
		if code := app.Run([]string{"status", "acme-lib"}); code != 0 {
			t.Fatalf("status %d exit %d: %s", i+1, code, errOut.String())
		}
	}
	if runner.clones != 1 {
		t.Fatalf("online status cloned %d times, want one reusable checkout", runner.clones)
	}
}

func TestSetPinUnpinSourceAndMovedAnnouncement(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	original := filepath.Join(tmp, "original.git")
	fork := filepath.Join(tmp, "fork.git")
	other := filepath.Join(tmp, "other.git")
	consumer := filepath.Join(tmp, "consumer")
	mustMkdir(t, source)
	git(t, source, "init", "-b", "main")
	git(t, source, "config", "user.email", "test@example.com")
	git(t, source, "config", "user.name", "Test")
	base := []byte("schema: 1\nmodule:\n  id: acme-lib-utils\n  repository: file:///canonical.git\n  release:\n    channel: main\n")
	mustWrite(t, filepath.Join(source, "a2amodule.yml"), base)
	git(t, source, "add", "a2amodule.yml")
	git(t, source, "commit", "-m", "one")
	tagCommit := strings.TrimSpace(gitOutput(t, source, "rev-parse", "HEAD"))
	git(t, source, "tag", "release")
	mustWrite(t, filepath.Join(source, "a2amodule.yml"), append(base, []byte("x-revision: two\n")...))
	git(t, source, "add", "a2amodule.yml")
	git(t, source, "commit", "-m", "two")
	git(t, source, "branch", "release")
	git(t, tmp, "clone", "--bare", source, original)
	git(t, tmp, "clone", "--bare", source, fork)
	mustMkdir(t, consumer)
	mustWrite(t, filepath.Join(consumer, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: acme-app-cli\n"))
	var out, errOut bytes.Buffer
	app := cli.New(&out, &errOut)
	app.Root = consumer
	if code := app.Run([]string{"add", "file://" + original, "--no-wire"}); code != 0 {
		t.Fatalf("add %d: %s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"set", "acme-lib-utils", "--git", "file://" + fork}); code != 0 {
		t.Fatalf("set git %d: %s", code, errOut.String())
	}
	m, err := manifest.Load(filepath.Join(consumer, "a2amodule.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Dependencies[0].Git != "file://"+fork {
		t.Fatal("source did not switch")
	}
	out.Reset()
	errOut.Reset()
	_ = app.Run([]string{"status", "acme-lib-utils", "--offline"})
	if !strings.Contains(out.String(), "fork of file:///canonical.git") || !strings.Contains(out.String(), "branch main") {
		t.Fatalf("fork status missing source/ref: %s", out.String())
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"set", "acme-lib-utils", "--ref", "release"}); code != 0 {
		t.Fatalf("set ref %d: %s", code, errOut.String())
	}
	l, _ := manifest.LoadLock(filepath.Join(consumer, "a2amodule.lock"))
	if l.Dependencies["acme-lib-utils"].Commit != tagCommit {
		t.Fatalf("ambiguous ref did not choose tag: %s want %s", l.Dependencies["acme-lib-utils"].Commit, tagCommit)
	}
	if !strings.Contains(errOut.String(), "selected refs/tags/release") {
		t.Fatalf("ambiguity not reported: %s", errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"pin", "acme-lib-utils"}); code != 0 {
		t.Fatalf("pin %d: %s", code, errOut.String())
	}
	m, _ = manifest.Load(filepath.Join(consumer, "a2amodule.yml"))
	if len(m.Dependencies[0].Ref) != 40 || m.Dependencies[0].Track != "locked" {
		t.Fatalf("not pinned: %#v", m.Dependencies[0])
	}
	out.Reset()
	errOut.Reset()
	_ = app.Run([]string{"status", "acme-lib-utils", "--offline"})
	if !strings.Contains(out.String(), "pinned "+tagCommit[:12]) {
		t.Fatalf("pinned status missing ref: %s", out.String())
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"unpin", "acme-lib-utils", "--ref", "main"}); code != 0 {
		t.Fatalf("unpin %d: %s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"unpin", "acme-lib-utils", "--ref", "main", "--track", "invalid"}); code != 2 {
		t.Fatalf("invalid unpin track exit %d: %s", code, errOut.String())
	}
	otherSource := filepath.Join(tmp, "other-source")
	mustMkdir(t, otherSource)
	git(t, otherSource, "init", "-b", "main")
	git(t, otherSource, "config", "user.email", "test@example.com")
	git(t, otherSource, "config", "user.name", "Test")
	mustWrite(t, filepath.Join(otherSource, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: different-module\n"))
	git(t, otherSource, "add", "a2amodule.yml")
	git(t, otherSource, "commit", "-m", "other")
	git(t, tmp, "clone", "--bare", otherSource, other)
	beforeManifest, _ := os.ReadFile(filepath.Join(consumer, "a2amodule.yml"))
	beforeLock, _ := os.ReadFile(filepath.Join(consumer, "a2amodule.lock"))
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"set", "acme-lib-utils", "--git", "file://" + other}); code != 1 {
		t.Fatalf("id mismatch exit %d", code)
	}
	afterManifest, _ := os.ReadFile(filepath.Join(consumer, "a2amodule.yml"))
	afterLock, _ := os.ReadFile(filepath.Join(consumer, "a2amodule.lock"))
	if !bytes.Equal(beforeManifest, afterManifest) || !bytes.Equal(beforeLock, afterLock) {
		t.Fatal("id mismatch changed metadata")
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"set", "acme-lib-utils", "--git", "file://" + original, "--ref", "main"}); code != 0 {
		t.Fatalf("back to original %d: %s", code, errOut.String())
	}
	moved := append(base, []byte("  moved-to:\n    git: file://"+fork+"\n")...)
	mustWrite(t, filepath.Join(source, "a2amodule.yml"), moved)
	git(t, source, "add", "a2amodule.yml")
	git(t, source, "commit", "-m", "moved")
	git(t, source, "push", original, "main")
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"update"}); code != 1 || !strings.Contains(errOut.String(), "moved to") {
		t.Fatalf("move not detected %d: %s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	_ = app.Run([]string{"status", "acme-lib-utils"})
	if !strings.Contains(out.String(), "moved → file://"+fork) {
		t.Fatalf("moved status missing source: %s", out.String())
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"update", "--follow-moves"}); code != 0 {
		t.Fatalf("follow move %d: %s", code, errOut.String())
	}
	m, _ = manifest.Load(filepath.Join(consumer, "a2amodule.yml"))
	if m.Dependencies[0].Git != "file://"+fork {
		t.Fatalf("move not followed: %#v", m.Dependencies[0])
	}
}

func TestAddRollbackAndWireRepair(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	bare := filepath.Join(tmp, "library.git")
	consumer := filepath.Join(tmp, "consumer")
	mustMkdir(t, source)
	git(t, source, "init", "-b", "main")
	git(t, source, "config", "user.email", "test@example.com")
	git(t, source, "config", "user.name", "Test")
	mustWrite(t, filepath.Join(source, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: acme-lib\n  release:\n    channel: main\n  exports:\n    - ecosystem: npm\n      name: '@acme/lib'\n    - ecosystem: pypi\n      name: acme-lib\n"))
	git(t, source, "add", "a2amodule.yml")
	git(t, source, "commit", "-m", "library")
	git(t, tmp, "clone", "--bare", source, bare)
	mustMkdir(t, consumer)
	mustWrite(t, filepath.Join(consumer, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: acme-app\n"))
	mustWrite(t, filepath.Join(consumer, "package.json"), []byte("{\n  \"name\": \"acme-app\",\n  \"dependencies\": {}\n}\n"))
	mustWrite(t, filepath.Join(consumer, "pyproject.toml"), []byte("[tool.invalid]\nvalue = true\n"))
	names := []string{"a2amodule.yml", "package.json", "pyproject.toml"}
	before := map[string][]byte{}
	for _, name := range names {
		before[name], _ = os.ReadFile(filepath.Join(consumer, name))
	}
	var out, errOut bytes.Buffer
	app := cli.New(&out, &errOut)
	app.Root = consumer
	if code := app.Run([]string{"add", "file://" + bare, "--wire", "npm,pypi"}); code != 1 {
		t.Fatalf("failing add exit %d: %s", code, errOut.String())
	}
	for _, name := range names {
		after, _ := os.ReadFile(filepath.Join(consumer, name))
		if !bytes.Equal(before[name], after) {
			t.Fatalf("%s changed after failed add\nbefore:\n%s\nafter:\n%s", name, before[name], after)
		}
	}
	if _, err := os.Stat(filepath.Join(consumer, "a2amodule.lock")); !os.IsNotExist(err) {
		t.Fatalf("lock exists after failed add: %v", err)
	}
	if _, err := os.Stat(filepath.Join(consumer, ".git-a2a", "cache", "acme-lib")); !os.IsNotExist(err) {
		t.Fatalf("cache exists after failed add: %v", err)
	}
	mustWrite(t, filepath.Join(consumer, "pyproject.toml"), []byte("[project]\nname = \"acme-app\"\ndependencies = []\n"))
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"add", "file://" + bare, "--no-wire"}); code != 0 {
		t.Fatalf("record-only add exit %d: %s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"wire", "acme-lib", "--ecosystem", "pypi"}); code != 0 {
		t.Fatalf("wire repair exit %d: %s", code, errOut.String())
	}
	pyproject, _ := os.ReadFile(filepath.Join(consumer, "pyproject.toml"))
	if !strings.Contains(string(pyproject), "acme-lib @ git+") {
		t.Fatalf("wire did not repair pyproject:\n%s", pyproject)
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"wire", "acme-lib", "--ecosystem", "pypi"}); code != 0 || !strings.Contains(errOut.String(), "wiring is current") {
		t.Fatalf("idempotent wire exit %d: %s", code, errOut.String())
	}
}

func TestUpdateCommitsEarlierDependencyAndLeavesFailingDependencyUntouched(t *testing.T) {
	tmp := t.TempDir()
	consumer := filepath.Join(tmp, "consumer")
	mustMkdir(t, consumer)
	mustWrite(t, filepath.Join(consumer, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: acme-app\n"))
	mustWrite(t, filepath.Join(consumer, "package.json"), []byte("{\n  \"name\": \"acme-app\",\n  \"dependencies\": {}\n}\n"))
	makeLibrary := func(id, packageName string) (string, string) {
		source := filepath.Join(tmp, id+"-source")
		bare := filepath.Join(tmp, id+".git")
		mustMkdir(t, source)
		git(t, source, "init", "-b", "main")
		git(t, source, "config", "user.email", "test@example.com")
		git(t, source, "config", "user.name", "Test")
		mustWrite(t, filepath.Join(source, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: "+id+"\n  release:\n    channel: main\n  exports:\n    - ecosystem: npm\n      name: '"+packageName+"'\n"))
		mustWrite(t, filepath.Join(source, "package.json"), []byte("{\"name\":\""+packageName+"\",\"version\":\"0.0.0\"}\n"))
		git(t, source, "add", ".")
		git(t, source, "commit", "-m", "initial")
		git(t, tmp, "clone", "--bare", source, bare)
		return source, bare
	}
	firstSource, firstBare := makeLibrary("acme-first", "@acme/first")
	_, secondBare := makeLibrary("acme-second", "@acme/second")
	var out, errOut bytes.Buffer
	app := cli.New(&out, &errOut)
	app.Root = consumer
	for _, bare := range []string{firstBare, secondBare} {
		out.Reset()
		errOut.Reset()
		if code := app.Run([]string{"add", "file://" + bare}); code != 0 {
			t.Fatalf("add %s exit %d: %s", bare, code, errOut.String())
		}
	}
	before, _ := manifest.LoadLock(filepath.Join(consumer, "a2amodule.lock"))
	firstOld := before.Dependencies["acme-first"].Commit
	secondOld := before.Dependencies["acme-second"].Commit
	manifestPath := filepath.Join(firstSource, "a2amodule.yml")
	firstManifest, _ := os.ReadFile(manifestPath)
	mustWrite(t, manifestPath, append(firstManifest, []byte("x-revision: two\n")...))
	git(t, firstSource, "add", "a2amodule.yml")
	git(t, firstSource, "commit", "-m", "update")
	git(t, firstSource, "push", firstBare, "main")
	own, _ := manifest.Load(filepath.Join(consumer, "a2amodule.yml"))
	for i := range own.Dependencies {
		if own.Dependencies[i].ID == "acme-second" {
			own.Dependencies[i].Git = "file:///definitely-missing/acme-second.git"
		}
	}
	ownRaw, _ := manifest.Marshal(own)
	mustWrite(t, filepath.Join(consumer, "a2amodule.yml"), ownRaw)
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"update"}); code != 1 {
		t.Fatalf("update exit %d: %s", code, errOut.String())
	}
	after, _ := manifest.LoadLock(filepath.Join(consumer, "a2amodule.lock"))
	if after.Dependencies["acme-first"].Commit == firstOld {
		t.Fatal("first dependency was not committed before second failed")
	}
	if after.Dependencies["acme-second"].Commit != secondOld {
		t.Fatal("failing dependency lock changed")
	}
	packageJSON, _ := os.ReadFile(filepath.Join(consumer, "package.json"))
	if !strings.Contains(string(packageJSON), after.Dependencies["acme-first"].Commit) || !strings.Contains(string(packageJSON), secondOld) {
		t.Fatalf("package wiring and lock disagree:\n%s", packageJSON)
	}
}

func TestUpdateFollowMovesProcessesEveryDependency(t *testing.T) {
	tmp := t.TempDir()
	consumer := filepath.Join(tmp, "consumer")
	mustMkdir(t, consumer)
	mustWrite(t, filepath.Join(consumer, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: acme-app\n"))
	type movedPair struct{ source, original, moved string }
	makePair := func(id string) movedPair {
		source := filepath.Join(tmp, id+"-source")
		original := filepath.Join(tmp, id+"-old.git")
		moved := filepath.Join(tmp, id+"-new.git")
		mustMkdir(t, source)
		git(t, source, "init", "-b", "main")
		git(t, source, "config", "user.email", "test@example.com")
		git(t, source, "config", "user.name", "Test")
		mustWrite(t, filepath.Join(source, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: "+id+"\n  release:\n    channel: main\n"))
		git(t, source, "add", "a2amodule.yml")
		git(t, source, "commit", "-m", "initial")
		git(t, tmp, "clone", "--bare", source, original)
		git(t, tmp, "clone", "--bare", source, moved)
		return movedPair{source: source, original: original, moved: moved}
	}
	pairs := []movedPair{makePair("acme-first"), makePair("acme-second")}
	var out, errOut bytes.Buffer
	app := cli.New(&out, &errOut)
	app.Root = consumer
	for _, pair := range pairs {
		if code := app.Run([]string{"add", "file://" + pair.original, "--no-wire"}); code != 0 {
			t.Fatalf("add exit %d: %s", code, errOut.String())
		}
		out.Reset()
		errOut.Reset()
	}
	for i, pair := range pairs {
		id := fmt.Sprintf("acme-%s", []string{"first", "second"}[i])
		mustWrite(t, filepath.Join(pair.source, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: "+id+"\n  moved-to:\n    git: file://"+pair.moved+"\n  release:\n    channel: main\n"))
		git(t, pair.source, "add", "a2amodule.yml")
		git(t, pair.source, "commit", "-m", "announce move")
		git(t, pair.source, "push", pair.original, "main")
	}
	if code := app.Run([]string{"update", "--follow-moves"}); code != 0 {
		t.Fatalf("follow moves exit %d: %s", code, errOut.String())
	}
	own, _ := manifest.Load(filepath.Join(consumer, "a2amodule.yml"))
	got := map[string]string{}
	for _, dep := range own.Dependencies {
		got[dep.ID] = dep.Git
	}
	for i, pair := range pairs {
		id := fmt.Sprintf("acme-%s", []string{"first", "second"}[i])
		if got[id] != "file://"+pair.moved {
			t.Fatalf("%s did not follow move: %q", id, got[id])
		}
	}
}

func TestCacheLossIsRecoverableBySetAndUpdate(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	bare := filepath.Join(tmp, "library.git")
	consumer := filepath.Join(tmp, "consumer")
	mustMkdir(t, source)
	git(t, source, "init", "-b", "main")
	git(t, source, "config", "user.email", "test@example.com")
	git(t, source, "config", "user.name", "Test")
	mustWrite(t, filepath.Join(source, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: acme-lib\n  release:\n    channel: main\nagents:\n  - name: acme-owner\n    role: owner\n    contacts:\n      - intents: [question]\n        kind: url\n        url: https://example.test/acme\n"))
	git(t, source, "add", "a2amodule.yml")
	git(t, source, "commit", "-m", "initial")
	git(t, tmp, "clone", "--bare", source, bare)
	mustMkdir(t, consumer)
	mustWrite(t, filepath.Join(consumer, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: acme-app\n"))
	var out, errOut bytes.Buffer
	app := cli.New(&out, &errOut)
	app.Root = consumer
	if code := app.Run([]string{"add", "file://" + bare, "--no-wire"}); code != 0 {
		t.Fatalf("add exit %d: %s", code, errOut.String())
	}
	cacheRoot := filepath.Join(consumer, ".git-a2a")
	if err := os.RemoveAll(cacheRoot); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"set", "acme-lib", "--ref", "main"}); code != 0 {
		t.Fatalf("set without cache exit %d: %s", code, errOut.String())
	}
	if err := os.RemoveAll(cacheRoot); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"update"}); code != 0 || !strings.Contains(out.String(), "restore cache") {
		t.Fatalf("cache repair update exit %d out=%s err=%s", code, out.String(), errOut.String())
	}
	for _, args := range [][]string{{"sync"}, {"who", "acme-lib", "--intent", "question"}} {
		out.Reset()
		errOut.Reset()
		if code := app.Run(args); code != 0 {
			t.Fatalf("%v after repair exit %d: %s", args, code, errOut.String())
		}
	}
}

func TestShowSurfaceRecordsFetchedTreeInLock(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	bare := filepath.Join(tmp, "library.git")
	consumer := filepath.Join(tmp, "consumer")
	mustMkdir(t, filepath.Join(source, "surface"))
	git(t, source, "init", "-b", "main")
	git(t, source, "config", "user.email", "test@example.com")
	git(t, source, "config", "user.name", "Test")
	mustWrite(t, filepath.Join(source, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: acme-lib\n  surface: surface/\n  release:\n    channel: main\n"))
	mustWrite(t, filepath.Join(source, "surface", "API.md"), []byte("public API\n"))
	git(t, source, "add", "a2amodule.yml", "surface/API.md")
	git(t, source, "commit", "-m", "surface")
	wantTree := strings.TrimSpace(gitOutput(t, source, "rev-parse", "HEAD:surface"))
	git(t, tmp, "clone", "--bare", source, bare)
	mustMkdir(t, consumer)
	mustWrite(t, filepath.Join(consumer, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: consumer\n"))
	var out, errOut bytes.Buffer
	app := cli.New(&out, &errOut)
	app.Root = consumer
	if code := app.Run([]string{"add", "file://" + bare, "--no-wire"}); code != 0 {
		t.Fatalf("add exit %d: %s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"show", "acme-lib", "--surface"}); code != 0 {
		t.Fatalf("show --surface exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "API.md\n") {
		t.Fatalf("surface listing missing: %q", out.String())
	}
	locked, err := manifest.LoadLock(filepath.Join(consumer, "a2amodule.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if got := locked.Dependencies["acme-lib"].Surface; got != "tree:"+wantTree {
		t.Fatalf("surface lock = %q, want tree:%s", got, wantTree)
	}
	if body, err := os.ReadFile(filepath.Join(consumer, ".git-a2a", "cache", "acme-lib", "surface", "API.md")); err != nil || string(body) != "public API\n" {
		t.Fatalf("surface body=%q err=%v", body, err)
	}
}

func TestAddRemoteWithoutManifestReturnsNothingResolved(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	bare := filepath.Join(tmp, "empty.git")
	consumer := filepath.Join(tmp, "consumer")
	mustMkdir(t, source)
	git(t, source, "init", "-b", "main")
	git(t, source, "config", "user.email", "test@example.com")
	git(t, source, "config", "user.name", "Test")
	mustWrite(t, filepath.Join(source, "README.md"), []byte("no manifest\n"))
	git(t, source, "add", "README.md")
	git(t, source, "commit", "-m", "no manifest")
	git(t, tmp, "clone", "--bare", source, bare)
	mustMkdir(t, consumer)
	mustWrite(t, filepath.Join(consumer, "a2amodule.yml"), []byte("schema: 1\nmodule: {id: consumer}\n"))
	var out, errOut bytes.Buffer
	app := cli.New(&out, &errOut)
	app.Root = consumer
	if code := app.Run([]string{"add", "file://" + bare}); code != 2 {
		t.Fatalf("exit %d, want 2: %s", code, errOut.String())
	}
}

func TestUpdateCheckPrintsAmbiguityAdvisoryBeforeReturning(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	bare := filepath.Join(tmp, "library.git")
	consumer := filepath.Join(tmp, "consumer")
	mustMkdir(t, source)
	git(t, source, "init", "-b", "main")
	git(t, source, "config", "user.email", "test@example.com")
	git(t, source, "config", "user.name", "Test")
	mustWrite(t, filepath.Join(source, "a2amodule.yml"), []byte("schema: 1\nmodule: {id: acme-lib}\n"))
	git(t, source, "add", "a2amodule.yml")
	git(t, source, "commit", "-m", "one")
	git(t, source, "tag", "release")
	git(t, source, "branch", "release")
	git(t, tmp, "clone", "--bare", source, bare)
	mustMkdir(t, consumer)
	mustWrite(t, filepath.Join(consumer, "a2amodule.yml"), []byte("schema: 1\nmodule: {id: consumer}\n"))
	var out, errOut bytes.Buffer
	app := cli.New(&out, &errOut)
	app.Root = consumer
	if code := app.Run([]string{"add", "file://" + bare + "#release", "--no-wire"}); code != 0 {
		t.Fatalf("add exit %d: %s", code, errOut.String())
	}
	mustWrite(t, filepath.Join(source, "a2amodule.yml"), []byte("schema: 1\nmodule: {id: acme-lib}\nx-revision: two\n"))
	git(t, source, "add", "a2amodule.yml")
	git(t, source, "commit", "-m", "two")
	git(t, source, "tag", "-f", "release")
	git(t, source, "push", bare, "main", "--force", "refs/tags/release")
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"update", "--check"}); code != 1 {
		t.Fatalf("update --check exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "dependency update(s) available") || !strings.Contains(errOut.String(), "ambiguous; selected refs/tags/release") {
		t.Fatalf("check advisories missing:\n%s", errOut.String())
	}
}

func TestRemoveDoesNotReportSuccessWhenMissingCacheCannotRecover(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "source")
	bare := filepath.Join(tmp, "library.git")
	consumer := filepath.Join(tmp, "consumer")
	mustMkdir(t, source)
	git(t, source, "init", "-b", "main")
	git(t, source, "config", "user.email", "test@example.com")
	git(t, source, "config", "user.name", "Test")
	mustWrite(t, filepath.Join(source, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: acme-lib\n  exports:\n    - ecosystem: npm\n      name: '@acme/lib'\n"))
	git(t, source, "add", "a2amodule.yml")
	git(t, source, "commit", "-m", "module")
	git(t, tmp, "clone", "--bare", source, bare)
	mustMkdir(t, consumer)
	mustWrite(t, filepath.Join(consumer, "a2amodule.yml"), []byte("schema: 1\nmodule: {id: consumer}\n"))
	mustWrite(t, filepath.Join(consumer, "package.json"), []byte("{\"name\":\"consumer\",\"dependencies\":{}}\n"))
	var out, errOut bytes.Buffer
	app := cli.New(&out, &errOut)
	app.Root = consumer
	if code := app.Run([]string{"add", "file://" + bare}); code != 0 {
		t.Fatalf("add exit %d: %s", code, errOut.String())
	}
	if err := os.RemoveAll(filepath.Join(consumer, ".git-a2a")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(bare); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := app.Run([]string{"remove", "acme-lib"}); code != 1 || strings.Contains(errOut.String(), "removed acme-lib") {
		t.Fatalf("remove exit %d: %s", code, errOut.String())
	}
	own, err := manifest.Load(filepath.Join(consumer, "a2amodule.yml"))
	if err != nil || len(own.Dependencies) != 1 {
		t.Fatalf("dependency metadata changed: %#v err=%v", own, err)
	}
	packageBytes, readErr := os.ReadFile(filepath.Join(consumer, "package.json"))
	if readErr != nil || !strings.Contains(string(packageBytes), "@acme/lib") {
		t.Fatal("wiring was removed before recovery failed")
	}
}

func TestPolyglotConsumerFullDependencyLifecycle(t *testing.T) {
	tmp := t.TempDir()
	goProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const marker = "/@v/"
		markerAt := strings.Index(r.URL.Path, marker)
		if markerAt < 1 {
			http.NotFound(w, r)
			return
		}
		modulePath := strings.TrimPrefix(r.URL.Path[:markerAt], "/")
		name := r.URL.Path[markerAt+len(marker):]
		switch {
		case strings.HasSuffix(name, ".info"):
			revision := strings.TrimSuffix(name, ".info")
			if len(revision) == 40 {
				fmt.Fprintf(w, `{"Version":"v0.0.0-20260822112233-%s","Time":"2026-08-22T11:22:33Z"}`, revision[:12])
				return
			}
			fmt.Fprintf(w, `{"Version":%q,"Time":"2026-08-22T11:22:33Z"}`, revision)
		case strings.HasSuffix(name, ".mod"):
			fmt.Fprintf(w, "module %s\n\ngo 1.24\n", modulePath)
		default:
			http.NotFound(w, r)
		}
	}))
	defer goProxy.Close()
	t.Setenv("GOPROXY", goProxy.URL)
	t.Setenv("GONOSUMDB", "example.test/acme/lib")
	source := filepath.Join(tmp, "source")
	original := filepath.Join(tmp, "original.git")
	fork := filepath.Join(tmp, "fork.git")
	consumer := filepath.Join(tmp, "consumer")
	publicOriginal := "https://example.test/acme/lib.git"
	publicFork := "https://mirror.example.test/acme/lib.git"
	mustMkdir(t, source)
	git(t, source, "init", "-b", "main")
	git(t, source, "config", "user.email", "test@example.com")
	git(t, source, "config", "user.name", "Test")
	manifestOne := []byte("schema: 1\nmodule:\n  id: acme-lib\n  repository: " + publicOriginal + "\n  release:\n    channel: main\n  exports:\n    - ecosystem: npm\n      name: '@acme/lib'\n    - ecosystem: pypi\n      name: acme-lib\n    - ecosystem: golang\n      name: example.test/acme/lib\n    - ecosystem: cargo\n      name: acme-lib\n    - ecosystem: swift\n      name: AcmeLib\n    - ecosystem: pub\n      name: acme_lib\nagents:\n  - name: owner\n    role: owner\n    contacts:\n      - intents: ['*']\n        kind: url\n        url: https://owner.example.test/\n")
	mustWrite(t, filepath.Join(source, "a2amodule.yml"), manifestOne)
	git(t, source, "add", "a2amodule.yml")
	git(t, source, "commit", "-m", "one")
	git(t, tmp, "clone", "--bare", source, original)
	git(t, tmp, "clone", "--bare", source, fork)
	mustMkdir(t, consumer)
	mustWrite(t, filepath.Join(consumer, "a2amodule.yml"), []byte("# keep this comment\nschema: 1\nmodule:\n  id: consumer\n  description: >-\n    folded consumer description\n  languages: [typescript, python, go]\nx-local: \"keep quoted\"\n"))
	mustWrite(t, filepath.Join(consumer, "package.json"), []byte("{\"name\":\"consumer\",\"dependencies\":{\"left-pad\":\"^1.0.0\"}}\n"))
	mustWrite(t, filepath.Join(consumer, "pyproject.toml"), []byte("[project]\nname = \"consumer\"\ndependencies = []\n"))
	mustWrite(t, filepath.Join(consumer, "go.mod"), []byte("module example.test/consumer\n\ngo 1.24\n"))
	mustWrite(t, filepath.Join(consumer, "Cargo.toml"), []byte("[package]\nname = \"consumer\"\nversion = \"0.1.0\"\n\n[dependencies]\nserde = \"1\"\n"))
	mustWrite(t, filepath.Join(consumer, "Package.swift"), []byte("// swift-tools-version: 6.0\nimport PackageDescription\n\nlet package = Package(\n    name: \"Consumer\",\n    dependencies: [],\n    targets: [.target(name: \"Consumer\")]\n)\n"))
	mustWrite(t, filepath.Join(consumer, "pubspec.yaml"), []byte("name: consumer\nenvironment:\n  sdk: ^3.8.0\ndependencies:\n  http: ^1.0.0\n"))
	var out, errOut bytes.Buffer
	app := cli.New(&out, &errOut)
	app.Root = consumer
	app.Runner = mappedLocalRunner{delegate: gitx.ExecRunner{}, remotes: map[string]string{
		publicOriginal: "file://" + original,
		publicFork:     "file://" + fork,
	}}
	var transcript strings.Builder
	run := func(args ...string) string {
		t.Helper()
		out.Reset()
		errOut.Reset()
		fmt.Fprintf(&transcript, "$ git-a2a %s\n", strings.Join(args, " "))
		if code := app.Run(args); code != 0 {
			t.Fatalf("git-a2a %v exit %d\nstdout:\n%s\nstderr:\n%s", args, code, out.String(), errOut.String())
		}
		transcript.WriteString(out.String())
		transcript.WriteString(errOut.String())
		return out.String() + errOut.String()
	}

	run("add", publicOriginal)
	assertPolyglotPins(t, consumer)
	run("sync")
	if text := run("status", "acme-lib", "--offline"); !strings.Contains(text, "npm clean, pypi clean, golang clean, cargo clean, swift clean, pub clean") {
		t.Fatalf("polyglot status is not clean:\n%s", text)
	}

	mustWrite(t, filepath.Join(source, "a2amodule.yml"), append(manifestOne, []byte("x-revision: two\n")...))
	git(t, source, "add", "a2amodule.yml")
	git(t, source, "commit", "-m", "two")
	git(t, source, "push", original, "main")
	run("update", "--review")
	assertPolyglotPins(t, consumer)
	run("set", "acme-lib", "--git", publicFork)
	assertPolyglotPins(t, consumer)
	run("pin", "acme-lib")
	run("unpin", "acme-lib", "--ref", "main")
	run("remove", "acme-lib")

	own, err := manifest.Load(filepath.Join(consumer, "a2amodule.yml"))
	if err != nil || len(own.Dependencies) != 0 {
		t.Fatalf("dependencies after remove: %#v err=%v", own, err)
	}
	manifestBytes, _ := os.ReadFile(filepath.Join(consumer, "a2amodule.yml"))
	for _, preserved := range []string{"# keep this comment", "description: >-", "languages: [typescript, python, go]", `x-local: "keep quoted"`} {
		if !strings.Contains(string(manifestBytes), preserved) {
			t.Errorf("manifest lost %q:\n%s", preserved, manifestBytes)
		}
	}
	packageBytes, _ := os.ReadFile(filepath.Join(consumer, "package.json"))
	if got, want := string(packageBytes), "{\"name\":\"consumer\",\"dependencies\":{\"left-pad\":\"^1.0.0\"}}\n"; got != want {
		t.Fatalf("package.json not restored\ngot  %s\nwant %s", got, want)
	}
	pyproject, _ := os.ReadFile(filepath.Join(consumer, "pyproject.toml"))
	if strings.Contains(string(pyproject), "acme-lib") {
		t.Fatalf("PyPI wiring remains:\n%s", pyproject)
	}
	goMod, _ := os.ReadFile(filepath.Join(consumer, "go.mod"))
	if strings.Contains(string(goMod), "example.test/acme/lib") {
		t.Fatalf("Go wiring remains:\n%s", goMod)
	}
	cargoManifest, _ := os.ReadFile(filepath.Join(consumer, "Cargo.toml"))
	if strings.Contains(string(cargoManifest), "acme-lib") {
		t.Fatalf("Cargo wiring remains:\n%s", cargoManifest)
	}
	swiftManifest, _ := os.ReadFile(filepath.Join(consumer, "Package.swift"))
	if strings.Contains(string(swiftManifest), publicOriginal) || strings.Contains(string(swiftManifest), publicFork) {
		t.Fatalf("Swift wiring remains:\n%s", swiftManifest)
	}
	pubspec, _ := os.ReadFile(filepath.Join(consumer, "pubspec.yaml"))
	if strings.Contains(string(pubspec), "acme_lib") {
		t.Fatalf("Pub wiring remains:\n%s", pubspec)
	}
	t.Logf("scenario output:\n%s", transcript.String())
}

type mappedLocalRunner struct {
	delegate gitx.Runner
	remotes  map[string]string
}

type archiveFailCountingRunner struct {
	delegate gitx.Runner
	clones   int
}

func (runner *archiveFailCountingRunner) Run(ctx context.Context, dir string, stdin []byte, args ...string) ([]byte, error) {
	if len(args) > 0 && args[0] == "archive" {
		return nil, fmt.Errorf("archive deliberately unavailable")
	}
	if len(args) > 0 && args[0] == "clone" {
		runner.clones++
	}
	return runner.delegate.Run(ctx, dir, stdin, args...)
}

func (runner mappedLocalRunner) Run(ctx context.Context, dir string, stdin []byte, args ...string) ([]byte, error) {
	mapped := append([]string(nil), args...)
	for i, argument := range mapped {
		for public, local := range runner.remotes {
			if argument == public {
				mapped[i] = local
			} else if argument == "--remote="+public {
				mapped[i] = "--remote=" + local
			}
		}
	}
	return runner.delegate.Run(ctx, dir, stdin, mapped...)
}

func assertPolyglotPins(t *testing.T, root string) {
	t.Helper()
	locked, err := manifest.LoadLock(filepath.Join(root, "a2amodule.lock"))
	if err != nil {
		t.Fatal(err)
	}
	commit := locked.Dependencies["acme-lib"].Commit
	for _, test := range []struct {
		file, pin string
	}{
		{"package.json", commit},
		{"pyproject.toml", commit},
		{"go.mod", commit[:12]},
		{"Cargo.toml", commit},
		{"Package.swift", commit},
		{"pubspec.yaml", commit},
	} {
		body, readErr := os.ReadFile(filepath.Join(root, test.file))
		if readErr != nil || !strings.Contains(string(body), test.pin) {
			t.Fatalf("%s does not contain locked pin %s: %v\n%s", test.file, test.pin, readErr, body)
		}
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}
func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}
func mustWrite(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
