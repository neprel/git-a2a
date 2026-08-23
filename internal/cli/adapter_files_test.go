package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdapterSnapshotsIncludeRootMSBuildProjects(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "Acme.App.csproj")
	original := []byte("<Project />\n")
	if err := os.WriteFile(project, original, 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot := snapshotAdapterFiles(root)
	if err := os.WriteFile(project, []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restoreAdapterFiles(root, snapshot)
	if got, err := os.ReadFile(project); err != nil || string(got) != string(original) {
		t.Fatalf("restored project = %q, %v", got, err)
	}
	stage := t.TempDir()
	copyAdapterFiles(root, stage)
	if got, err := os.ReadFile(filepath.Join(stage, "Acme.App.csproj")); err != nil || string(got) != string(original) {
		t.Fatalf("staged project = %q, %v", got, err)
	}
}
