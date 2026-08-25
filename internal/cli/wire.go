package cli

import (
	"fmt"
	"strings"

	"github.com/neprel/git-a2a/internal/cache"
	lockfile "github.com/neprel/git-a2a/internal/lock"
	"github.com/neprel/git-a2a/internal/manifest"
	vendortransport "github.com/neprel/git-a2a/internal/vendor"
)

func (a *App) wire(args []string) int {
	id, ecosystem := "", ""
	noRefresh := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--ecosystem":
			if i+1 >= len(args) {
				fmt.Fprintln(a.Err, "wire: --ecosystem needs a value")
				return 2
			}
			i++
			ecosystem = args[i]
		case "--no-refresh":
			noRefresh = true
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
	own, err := manifest.LoadDir(root)
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
		module, loadErr := manifest.LoadDir(cache.Dir(root, original.ID))
		if loadErr != nil {
			fmt.Fprintf(a.Err, "wire: dependency %s cache: %v\n", original.ID, loadErr)
			return 1
		}
		if original.Vendor != nil {
			vendorLock, vendorErr := (vendortransport.Manager{Runner: a.runner()}).Apply(a.context(), root, own, original, entry, false)
			if vendorErr != nil {
				fmt.Fprintf(a.Err, "wire: %s vendor: %v\n", original.ID, vendorErr)
				return 1
			}
			if entry.Vendor == nil || vendorLock.Mode != entry.Vendor.Mode || vendorLock.Path != entry.Vendor.Path || vendorLock.Tree != entry.Vendor.Tree {
				fmt.Fprintf(a.Err, "wire: %s vendored content does not match a2amodule.lock\n", original.ID)
				return 1
			}
		}
		dep := original
		if ecosystem != "" {
			selection := []string{ecosystem}
			dep.Wire = &selection
		}
		snapshots := snapshotAdapterFiles(root)
		outcomes, wireErr := wireAll(a.context(), root, dep, module, entry, !noRefresh)
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
		fmt.Fprintln(a.Err, wireEntrySummary(changed))
	}
	for _, line := range output {
		fmt.Fprintln(a.Out, line)
	}
	if err := refreshExistingManagedBlock(root); err != nil {
		fmt.Fprintf(a.Err, "wire: refresh managed block: %v\n", err)
		return 1
	}
	return 0
}

func wireEntrySummary(count int) string {
	return fmt.Sprintf("wired %d ecosystem %s", count, entryNoun(count))
}
