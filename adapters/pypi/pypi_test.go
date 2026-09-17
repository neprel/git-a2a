package pypi

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/neprel/git-a2a/v2/internal/adapter"
)

func TestSyncNativeReconcilesManagerLockBeforeEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	for _, test := range []struct {
		name, variant string
		want          []string
	}{
		{"poetry-pull", "poetry", []string{"lock --no-interaction", "sync --no-root --no-interaction"}},
		{"pdm-pull", "pdm", []string{"update --no-self --update-reuse fixture-py"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			log := filepath.Join(root, "commands.log")
			writeExecutable(t, filepath.Join(root, "bin", test.variant), "#!/bin/sh\nif [ \"$1\" = --version ]; then echo version 2.2.1; exit 0; fi\nprintf '%s\\n' \"$*\" >> \"$COMMAND_LOG\"\n")
			t.Setenv("COMMAND_LOG", log)
			t.Setenv("PATH", filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
			if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(managerProject(test.variant)), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := (Adapter{}).syncNative(context.Background(), root, adapter.Export{Name: "fixture-py"}); err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(body)), "\n")
			if strings.Join(lines, "|") != strings.Join(test.want, "|") {
				t.Fatalf("commands = %#v, want %#v", lines, test.want)
			}
		})
	}
}

func TestPEP621SyncResolvesDependencies(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	root := t.TempDir()
	log := filepath.Join(root, "python.log")
	writeExecutable(t, filepath.Join(root, "bin", "pip"), "#!/bin/sh\necho pip 25.2\n")
	writeExecutable(t, venvPythonPath(root), "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$COMMAND_LOG\"\n")
	t.Setenv("COMMAND_LOG", log)
	t.Setenv("PATH", filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	project := "[project]\nname = \"consumer\"\ndependencies = [\"fixture-py @ git+https://example.test/fixture.git@" + strings.Repeat("a", 40) + "\", \"idna==3.10\"]\n"
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(project), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (Adapter{}).syncNative(context.Background(), root, adapter.Export{Name: "fixture-py"}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	command := string(body)
	if strings.Contains(command, "--no-deps") || !strings.Contains(command, "-m pip install --force-reinstall") || strings.Count(command, "-m pip install") != 2 {
		t.Fatalf("pip command does not resolve dependencies: %s", command)
	}
	if strings.Contains(command, "fixture-py @ git+") || !strings.Contains(command, "git+https://example.test/fixture.git@") || !strings.Contains(command, "idna==3.10") {
		t.Fatalf("pip command does not separate the VCS target from unrelated requirements: %s", command)
	}
}

func TestPDMRemoveUsesTargetedNativeOperation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	root := t.TempDir()
	log := filepath.Join(root, "commands.log")
	writeExecutable(t, filepath.Join(root, "bin", "pdm"), "#!/bin/sh\nif [ \"$1\" = --version ]; then echo PDM 2.26.2; exit 0; fi\nprintf '%s\\n' \"$*\" >> \"$COMMAND_LOG\"\n")
	t.Setenv("COMMAND_LOG", log)
	t.Setenv("PATH", filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(managerProjectWithDependency("pdm", "fixture-py", "https://example.test/fixture.git", strings.Repeat("a", 40))), 0o644); err != nil {
		t.Fatal(err)
	}
	change, err := (Adapter{}).Remove(context.Background(), root, adapter.Dependency{}, adapter.Export{Name: "fixture-py"}, adapter.Locked{})
	if err != nil || !change.Changed {
		t.Fatalf("Remove() = %#v, %v", change, err)
	}
	body, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(body)); got != "remove --no-self fixture-py" {
		t.Fatalf("pdm command = %q", got)
	}
}

func TestEnsurePDMEnvironmentRecreatesOnlySelectedMissingVenv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	root := t.TempDir()
	environment := filepath.Join(t.TempDir(), "project-venv")
	selected := filepath.Join(environment, "bin", "python")
	if err := os.WriteFile(filepath.Join(root, ".pdm-python"), []byte(selected+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "venv.log")
	writeExecutable(t, filepath.Join(root, "bin", "python3"), "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$COMMAND_LOG\"\n")
	t.Setenv("COMMAND_LOG", log)
	t.Setenv("PATH", filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := ensurePDMEnvironment(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(body)), "-m venv "+environment; got != want {
		t.Fatalf("venv command = %q, want %q", got, want)
	}
}

func TestInspectExternalManagerEnvironmentUsesLockAndDirectURL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	for _, variant := range []string{"poetry", "pdm"} {
		t.Run(variant, func(t *testing.T) {
			root := t.TempDir()
			commit := strings.Repeat("c", 40)
			gitURL := "https://example.test/acme/fixture.git"
			name := "fixture-py"
			python := filepath.Join(root, "external", "bin", "python")
			metadata := `{"url":"` + gitURL + `","vcs_info":{"vcs":"git","commit_id":"` + commit + `"}}`
			writeExecutable(t, python, "#!/bin/sh\nprintf '%s\\n' \"$DIRECT_URL\"\n")
			writeExecutable(t, filepath.Join(root, "bin", variant), managerDiscoveryScript(variant))
			t.Setenv("DIRECT_URL", metadata)
			t.Setenv("PROJECT_PYTHON", python)
			t.Setenv("PATH", filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
			if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(managerProjectWithDependency(variant, name, gitURL, commit)), 0o644); err != nil {
				t.Fatal(err)
			}
			if variant == "pdm" {
				if err := os.WriteFile(filepath.Join(root, ".pdm-python"), []byte(python+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			lockName := map[string]string{"poetry": "poetry.lock", "pdm": "pdm.lock"}[variant]
			lock := "[[package]]\nname = \"" + name + "\"\nversion = \"1.0.0\"\n[package.source]\ntype = \"git\"\nurl = \"" + gitURL + "\"\nresolved_reference = \"" + commit + "\"\n"
			if err := os.WriteFile(filepath.Join(root, lockName), []byte(lock), 0o644); err != nil {
				t.Fatal(err)
			}
			dep := adapter.Dependency{Git: gitURL}
			exp := adapter.Export{Adapter: "pypi", Name: name}
			locked := adapter.Locked{Git: gitURL, Commit: commit}
			findings, err := (Adapter{}).Inspect(context.Background(), root, dep, exp, locked)
			if err != nil || len(findings) != 0 {
				t.Fatalf("Inspect() = %#v, %v", findings, err)
			}

			staleMetadata := `{"url":"` + gitURL + `","vcs_info":{"vcs":"git","commit_id":"` + strings.Repeat("d", 40) + `"}}`
			t.Setenv("DIRECT_URL", staleMetadata)
			findings, err = (Adapter{}).Inspect(context.Background(), root, dep, exp, locked)
			if err != nil || len(findings) != 1 || findings[0].File != variant+" project environment" {
				t.Fatalf("stale Inspect() = %#v, %v", findings, err)
			}
		})
	}
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func managerProject(variant string) string {
	if variant == "poetry" {
		return "[tool.poetry]\nname = \"consumer\"\nversion = \"0.1.0\"\n[tool.poetry.dependencies]\npython = \"^3.11\"\n"
	}
	return "[project]\nname = \"consumer\"\ndependencies = []\n[tool.pdm]\ndistribution = false\n"
}

func managerProjectWithDependency(variant, name, gitURL, commit string) string {
	if variant == "poetry" {
		return managerProject(variant) + name + " = { git = \"" + gitURL + "\", rev = \"" + commit + "\" }\n"
	}
	return "[project]\nname = \"consumer\"\ndependencies = [\"" + name + " @ git+" + gitURL + "@" + commit + "\"]\n[tool.pdm]\ndistribution = false\n"
}

func managerDiscoveryScript(variant string) string {
	if variant == "poetry" {
		return "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'Poetry 2.2.1'; else printf '%s\\n' \"$PROJECT_PYTHON\"; fi\n"
	}
	return "#!/bin/sh\necho 'PDM 2.26.2'\n"
}

func TestWireGoldenIdempotentUnwire(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join("..", "..", "testdata", "consumer-uv")
	original, err := os.ReadFile(filepath.Join(fixture, "pyproject.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "pyproject.toml"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "uv.lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{Name: "lib-utils", Git: "https://github.com/acme/lib-utils.git", Ref: "main"}
	exp := adapter.Export{Adapter: "pypi", Name: "acme-lib-utils"}
	locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
	a := Adapter{}
	change, err := a.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || !change.Changed {
		t.Fatalf("wire: %#v %v", change, err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "pyproject.toml"))
	want, _ := os.ReadFile(filepath.Join(fixture, "pyproject.golden.toml"))
	if string(got) != string(want) {
		t.Fatalf("golden differs\ngot:\n%s\nwant:\n%s", got, want)
	}
	change, err = a.Wire(context.Background(), root, dep, exp, locked)
	if err != nil || change.Changed {
		t.Fatalf("second wire: %#v %v", change, err)
	}
	if findings, err := a.Drift(context.Background(), root, dep, exp, locked); err != nil || len(findings) != 0 {
		t.Fatalf("clean drift: %v %v", findings, err)
	}
	wrong := locked
	wrong.Git = "https://github.com/acme/fork.git"
	if findings, err := a.Drift(context.Background(), root, dep, exp, wrong); err != nil || len(findings) != 1 {
		t.Fatalf("source drift: %v %v", findings, err)
	}
	change, err = a.Unwire(context.Background(), root, dep, exp)
	if err != nil || !change.Changed {
		t.Fatalf("unwire: %#v %v", change, err)
	}
	got, _ = os.ReadFile(filepath.Join(root, "pyproject.toml"))
	if string(got) != string(original) {
		t.Fatalf("unwire differs\ngot:\n%s\nwant:\n%s", got, original)
	}
}

func TestDriftMissingEntryIsUnwired(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte("[project]\nname = \"consumer\"\ndependencies = []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err := (Adapter{}).Drift(context.Background(), root,
		adapter.Dependency{Git: "https://github.com/acme/lib.git"},
		adapter.Export{Adapter: "pypi", Name: "acme-lib"},
		adapter.Locked{Git: "https://github.com/acme/lib.git", Commit: strings.Repeat("a", 40)})
	if err != nil || len(findings) != 1 {
		t.Fatalf("findings=%v err=%v", findings, err)
	}
}

func TestDetectPDMProjectWithoutLockfile(t *testing.T) {
	root := t.TempDir()
	content := "[project]\nname = \"consumer\"\n\n[tool.pdm]\ndistribution = true\n"
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, got, err := (Adapter{}).Detect(root)
	if err != nil || !ok || got != "pdm" {
		t.Fatalf("Detect() = %v, %q, %v", ok, got, err)
	}
}

func TestWireUsesLockedSourceAndExactCommit(t *testing.T) {
	root := t.TempDir()
	content := "[project]\nname = \"consumer\"\ndependencies = []\n"
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("c", 40)
	dep := adapter.Dependency{Git: "https://stale.example.test/lib.git", Ref: "main"}
	exp := adapter.Export{Adapter: "pypi", Name: "acme-lib"}
	locked := adapter.Locked{Git: "https://canonical.example.test/lib.git", Commit: commit}
	if _, err := (Adapter{}).Wire(context.Background(), root, dep, exp, locked); err != nil {
		t.Fatal(err)
	}
	got := string(mustReadPyProject(t, root))
	if !strings.Contains(got, "git+"+locked.Git+"@"+commit) || strings.Contains(got, dep.Git) || strings.Contains(got, "@main") {
		t.Fatalf("dependency was not pinned to the locked source and commit:\n%s", got)
	}
}

func mustReadPyProject(t *testing.T, root string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "pyproject.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWireConvertsInlineDependencyArraysOnly(t *testing.T) {
	for _, initial := range []string{
		"[project]\nname = \"consumer\"\ndependencies = []\n[tool.other]\nitems = []\n",
		"[project]\nname = \"consumer\"\ndependencies = [\"click>=8\", \"rich\"]\n[tool.other]\nitems = []\n",
	} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(initial), 0o644); err != nil {
			t.Fatal(err)
		}
		dep := adapter.Dependency{Git: "https://github.com/acme/lib.git", Ref: "main"}
		exp := adapter.Export{Adapter: "pypi", Name: "acme-lib"}
		locked := adapter.Locked{Git: dep.Git, Commit: strings.Repeat("a", 40)}
		if _, err := (Adapter{}).Wire(context.Background(), root, dep, exp, locked); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(filepath.Join(root, "pyproject.toml"))
		if !strings.Contains(string(got), "dependencies = [\n  \"") || !strings.Contains(string(got), "acme-lib @ git+") {
			t.Fatalf("inline array not converted:\n%s", got)
		}
		if !strings.Contains(string(got), "[tool.other]\nitems = []") {
			t.Fatalf("unrelated array changed:\n%s", got)
		}
	}
}

func TestWireUpdatesExistingPEP621GitPin(t *testing.T) {
	root := t.TempDir()
	oldCommit := strings.Repeat("a", 40)
	newCommit := strings.Repeat("b", 40)
	content := "[project]\nname = \"consumer\"\ndependencies = [\n  \"left-pad>=1\",\n  \"acme-lib @ git+https://example.test/acme/lib.git@" + oldCommit + "\",\n]\n"
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{Git: "https://mirror.example.test/acme/lib.git"}
	exp := adapter.Export{Adapter: "pypi", Name: "acme-lib"}
	change, err := (Adapter{}).Wire(context.Background(), root, dep, exp, adapter.Locked{Git: dep.Git, Commit: newCommit})
	if err != nil || !change.Changed {
		t.Fatalf("change=%#v err=%v", change, err)
	}
	updated, readErr := os.ReadFile(filepath.Join(root, "pyproject.toml"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	got := string(updated)
	if strings.Contains(got, oldCommit) || !strings.Contains(got, "git+"+dep.Git+"@"+newCommit) || !strings.Contains(got, "left-pad>=1") {
		t.Fatalf("dependency was not updated minimally:\n%s", got)
	}
}

func TestWireUpdatesBareUVSourceWithoutDuplicatingOrJoiningHeader(t *testing.T) {
	root := t.TempDir()
	oldCommit := strings.Repeat("a", 40)
	newCommit := strings.Repeat("b", 40)
	content := "[project]\nname = \"consumer\"\ndependencies = [\"acme_lib\"]\n\n[tool.uv.sources]\nacme_lib = { git = \"https://example.test/acme/lib.git\", rev = \"" + oldCommit + "\" }\n\n[tool.other]\nkeep = true\n"
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "uv.lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	dep := adapter.Dependency{Git: "https://example.test/acme/lib.git"}
	exp := adapter.Export{Adapter: "pypi", Name: "acme_lib"}
	if _, err := (Adapter{}).Wire(context.Background(), root, dep, exp, adapter.Locked{Git: dep.Git, Commit: newCommit}); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(filepath.Join(root, "pyproject.toml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(updated)
	if strings.Count(got, "rev =") != 1 || strings.Contains(got, oldCommit) || !strings.Contains(got, "[tool.uv.sources]\n\"acme_lib\" =") {
		t.Fatalf("UV source was not replaced minimally:\n%s", got)
	}
	if !strings.Contains(got, "\n[tool.other]\nkeep = true") {
		t.Fatalf("following table was changed:\n%s", got)
	}
	if findings, err := (Adapter{}).Drift(context.Background(), root, dep, exp, adapter.Locked{Git: dep.Git, Commit: newCommit}); err != nil || len(findings) != 0 {
		t.Fatalf("drift=%v err=%v", findings, err)
	}
}
