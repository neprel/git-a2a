package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/neprel/git-a2a/internal/manifest"
)

type App struct{ Out, Err io.Writer }

func New(out, errOut io.Writer) *App { return &App{Out: out, Err: errOut} }

func (a *App) Run(args []string) int {
	if len(args) == 0 {
		a.usage()
		return 2
	}
	switch args[0] {
	case "validate":
		return a.validate(args[1:])
	case "fmt":
		return a.format(args[1:])
	case "help", "-h", "--help":
		a.usage()
		return 0
	default:
		fmt.Fprintf(a.Err, "unknown command %q\n", args[0])
		a.usage()
		return 2
	}
}

func (a *App) usage() { fmt.Fprintln(a.Out, "usage: git-a2a <validate|fmt> [options]") }

func (a *App) validate(paths []string) int {
	if len(paths) == 0 {
		if _, err := os.Stat("a2amodule.yml"); err == nil {
			paths = []string{"a2amodule.yml"}
		}
		if _, err := os.Stat("a2amodule.lock"); err == nil {
			paths = append(paths, "a2amodule.lock")
		}
	}
	if len(paths) == 0 {
		fmt.Fprintln(a.Err, "no manifest or lock found")
		return 2
	}
	failed := false
	for _, path := range paths {
		var err error
		if strings.HasSuffix(path, ".lock") {
			_, err = manifest.LoadLock(path)
		} else {
			_, err = manifest.Load(path)
		}
		if err != nil {
			failed = true
			fmt.Fprintf(a.Err, "%s: %v\n", path, err)
		} else {
			fmt.Fprintf(a.Out, "%s: valid\n", path)
		}
	}
	if failed {
		fmt.Fprintf(a.Err, "%d file(s): validation failed\n", len(paths))
		return 1
	}
	fmt.Fprintf(a.Err, "%d file(s): valid\n", len(paths))
	return 0
}

func (a *App) format(args []string) int {
	check := false
	for _, arg := range args {
		if arg == "--check" {
			check = true
		} else {
			fmt.Fprintf(a.Err, "fmt: unknown argument %q\n", arg)
			return 2
		}
	}
	path := "a2amodule.yml"
	original, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(a.Err, "no manifest found")
		return 2
	}
	m, err := manifest.Parse(original)
	if err != nil {
		fmt.Fprintf(a.Err, "%s: %v\n", path, err)
		return 1
	}
	formatted, err := manifest.Marshal(m)
	if err != nil {
		fmt.Fprintf(a.Err, "fmt: %v\n", err)
		return 1
	}
	formatted = append(formatted, '\n')
	if string(original) == string(formatted) {
		fmt.Fprintln(a.Err, "manifest is canonical")
		return 0
	}
	if check {
		fmt.Fprintln(a.Err, "manifest is not canonical")
		return 1
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".a2amodule-*.tmp")
	if err != nil {
		fmt.Fprintf(a.Err, "fmt: %v\n", err)
		return 1
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(formatted); err == nil {
		err = tmp.Close()
	}
	if err == nil {
		err = os.Rename(tmpName, path)
	}
	if err != nil {
		fmt.Fprintf(a.Err, "fmt: %v\n", err)
		return 1
	}
	fmt.Fprintln(a.Err, "manifest formatted")
	return 0
}
