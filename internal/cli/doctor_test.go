package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDoctorReportsMissingDetectedToolWithFakePATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake POSIX executable")
	}
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "git"), "#!/bin/sh\necho 'git version 2.51.0'\n")
	t.Setenv("PATH", bin)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{\"name\":\"consumer-app\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"doctor"}); code != 1 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "npm: npm not found — install:") {
		t.Fatalf("stdout=%s", out.String())
	}
	if !strings.Contains(errOut.String(), "1 missing or incompatible") {
		t.Fatalf("stderr=%s", errOut.String())
	}
}

func TestDoctorJSONIsReadyWithOnlySupportedGit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake POSIX executable")
	}
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "git"), "#!/bin/sh\necho 'git version 2.51.0'\n")
	t.Setenv("PATH", bin)
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = t.TempDir()
	if code := app.Run([]string{"doctor", "--json"}); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Ready || len(report.Tools) != 1 || report.Tools[0].Command != "git" {
		t.Fatalf("report=%+v", report)
	}
}

func TestStatusVerboseShowsMissingDetectedTool(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake POSIX PATH")
	}
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a2amodule.yml"), []byte("schema: 1\nmodule:\n  id: consumer-app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{\"name\":\"consumer-app\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"status", "--offline", "-v"}); code != 1 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "npm: tool missing (npm) — install:") {
		t.Fatalf("stdout=%s", out.String())
	}
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}
