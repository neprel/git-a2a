package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/neprel/git-a2a/internal/catalog"
	lockfile "github.com/neprel/git-a2a/internal/lock"
	"github.com/neprel/git-a2a/internal/manifest"
)

func (a *App) catalog(args []string) int {
	if len(args) == 0 || args[0] != "export" {
		fmt.Fprintln(a.Err, "catalog: expected export")
		return 2
	}
	outPath := ""
	for i := 1; i < len(args); i++ {
		if args[i] == "--out" {
			if i+1 >= len(args) {
				fmt.Fprintln(a.Err, "catalog export: --out needs a file")
				return 2
			}
			i++
			outPath = args[i]
		} else {
			fmt.Fprintf(a.Err, "catalog export: unknown argument %s\n", args[i])
			return 2
		}
	}
	root := a.root()
	m, err := manifest.Load(filepath.Join(root, "a2amodule.yml"))
	if err != nil {
		fmt.Fprintf(a.Err, "catalog export: %v\n", err)
		return 2
	}
	repository := m.Module.Repository
	if repository == "" {
		if output, runErr := a.runner().Run(a.context(), root, nil, "config", "--get", "remote.origin.url"); runErr == nil {
			repository = strings.TrimSpace(string(output))
		}
	}
	repository = stripURLUserinfo(repository)
	ref := ""
	for _, agent := range m.Agents {
		if agent.Card == "" {
			ref = a.exportRef(m, repository)
			break
		}
	}
	value, err := catalog.Build(m, root, repository, ref)
	if err != nil {
		fmt.Fprintf(a.Err, "catalog export: %v\n", err)
		return 2
	}
	raw, err := catalog.Marshal(value)
	if err != nil {
		fmt.Fprintf(a.Err, "catalog export: %v\n", err)
		return 1
	}
	if outPath == "" {
		_, _ = a.Out.Write(raw)
	} else {
		if !filepath.IsAbs(outPath) {
			outPath = filepath.Join(root, outPath)
		}
		if err = lockfile.Atomic(outPath, raw, 0o644); err != nil {
			fmt.Fprintf(a.Err, "catalog export: %v\n", err)
			return 1
		}
	}
	fmt.Fprintln(a.Err, catalogEntrySummary(len(value.Entries)))
	return 0
}

func catalogEntrySummary(count int) string {
	return fmt.Sprintf("exported %d A2A catalog %s", count, entryNoun(count))
}

func entryNoun(count int) string {
	if count == 1 {
		return "entry"
	}
	return "entries"
}
