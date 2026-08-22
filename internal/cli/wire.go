package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/neprel/git-a2a/internal/cache"
	lockfile "github.com/neprel/git-a2a/internal/lock"
	"github.com/neprel/git-a2a/internal/manifest"
)

func (a *App) wire(args []string) int {
	id, ecosystem := "", ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--ecosystem":
			if i+1 >= len(args) {
				fmt.Fprintln(a.Err, "wire: --ecosystem needs a value")
				return 2
			}
			i++
			ecosystem = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				fmt.Fprintf(a.Err, "wire: unknown option %s\n", args[i])
				return 2
			}
			if id != "" {
				fmt.Fprintln(a.Err, "wire: expected at most one dependency id")
				return 2
			}
			id = args[i]
		}
	}
	root := a.root()
	own, err := manifest.Load(filepath.Join(root, "a2amodule.yml"))
	if err != nil {
		fmt.Fprintf(a.Err, "wire: own manifest: %v\n", err)
		return 2
	}
	locked, err := lockfile.Load(root)
	if err != nil {
		fmt.Fprintf(a.Err, "wire: lock: %v\n", err)
		return 1
	}
	matched := 0
	changed := 0
	var output []string
	for _, original := range own.Dependencies {
		if id != "" && original.ID != id {
			continue
		}
		matched++
		entry, ok := locked.Dependencies[original.ID]
		if !ok {
			fmt.Fprintf(a.Err, "wire: dependency %s is not locked\n", original.ID)
			return 1
		}
		module, loadErr := manifest.Load(filepath.Join(cache.Dir(root, original.ID), "a2amodule.yml"))
		if loadErr != nil {
			fmt.Fprintf(a.Err, "wire: dependency %s cache: %v\n", original.ID, loadErr)
			return 1
		}
		dep := original
		if ecosystem != "" {
			selection := []string{ecosystem}
			dep.Wire = &selection
		}
		snapshots := snapshotAdapterFiles(root)
		outcomes, wireErr := wireAll(a.context(), root, dep, module, entry, true)
		if wireErr != nil {
			restoreAdapterFiles(root, snapshots)
			fmt.Fprintf(a.Err, "wire: %s failed and was rolled back: %v\n", original.ID, wireErr)
			return 1
		}
		for _, outcome := range outcomes {
			if outcome.Warning != "" {
				fmt.Fprintf(a.Err, "warning: %s\n", outcome.Warning)
			}
			if outcome.Changed {
				changed++
				output = append(output, fmt.Sprintf("%s: wired %s", outcome.Ecosystem, original.ID))
			} else if !outcome.Wired {
				output = append(output, fmt.Sprintf("%s: not wired: %s", outcome.Ecosystem, outcome.Reason))
			}
		}
	}
	if matched == 0 {
		fmt.Fprintln(a.Err, "wire: no dependencies matched")
		return 2
	}
	if changed == 0 {
		fmt.Fprintln(a.Err, "wiring is current")
	} else {
		fmt.Fprintf(a.Err, "wired %d ecosystem entrie(s)\n", changed)
	}
	for _, line := range output {
		fmt.Fprintln(a.Out, line)
	}
	return 0
}
