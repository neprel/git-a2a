package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/neprel/git-a2a/adapters"
	"github.com/neprel/git-a2a/internal/adapter"
	"github.com/neprel/git-a2a/internal/manifest"
)

type doctorReport struct {
	Ready bool                 `json:"ready"`
	Tools []adapter.ToolStatus `json:"tools"`
}

func (a *App) doctor(args []string) int {
	jsonOut := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOut = true
		default:
			fmt.Fprintf(a.Err, "doctor: unknown option %s\n", arg)
			return 2
		}
	}
	requirements, err := doctorRequirements(a.root())
	if err != nil {
		fmt.Fprintf(a.Err, "doctor: %v\n", err)
		return 1
	}
	report := doctorReport{Ready: true}
	git := adapter.InspectTool(a.context(), adapter.GitTool())
	report.Tools = append(report.Tools, git)
	if !git.Ready {
		report.Ready = false
	}
	for _, requirement := range requirements {
		status := adapter.InspectTool(a.context(), requirement)
		report.Tools = append(report.Tools, status)
		if !status.Ready {
			report.Ready = false
		}
	}
	if jsonOut {
		body, _ := json.MarshalIndent(report, "", "  ")
		fmt.Fprintln(a.Out, string(body))
	} else {
		for _, status := range report.Tools {
			if !status.Found {
				fmt.Fprintln(a.Out, adapter.MissingToolError{Requirement: status.ToolRequirement}.Error())
				continue
			}
			state := "found"
			version := status.Version
			if !status.Ready {
				state = "too old"
			} else if status.Ecosystem == "git" && !adapter.VersionAtLeast(status.Version, "2.27.0") {
				state = "found (2.27+ preferred)"
			}
			fmt.Fprintf(a.Out, "%-10s %-24s %s", status.Command, version, state)
			fmt.Fprintf(a.Out, " %s", status.Path)
			fmt.Fprintln(a.Out)
			if !status.Ready {
				fmt.Fprintf(a.Out, "  install: %s\n", status.Install)
			}
		}
	}
	if report.Ready {
		fmt.Fprintf(a.Err, "%d prerequisite(s): ready\n", len(report.Tools))
		return 0
	}
	missing := 0
	for _, status := range report.Tools {
		if !status.Ready {
			missing++
		}
	}
	fmt.Fprintf(a.Err, "%d prerequisite(s): %d missing or incompatible\n", len(report.Tools), missing)
	return 1
}

func doctorRequirements(root string) ([]adapter.ToolRequirement, error) {
	byTool := map[string]adapter.ToolRequirement{}
	detected := map[string]adapter.Variant{}
	known := map[string]bool{}
	for _, implementation := range adapters.All() {
		known[implementation.Ecosystem()] = true
		ok, variant, err := implementation.Detect(root)
		if err != nil {
			return nil, err
		}
		if ok {
			detected[implementation.Ecosystem()] = variant
			tool := adapter.ToolFor(implementation.Ecosystem(), variant)
			byTool[tool.Command] = tool
		}
	}
	if own, err := manifest.Load(filepath.Join(root, "a2amodule.yml")); err == nil {
		for _, dep := range own.Dependencies {
			if dep.Wire == nil {
				continue
			}
			for _, ecosystem := range *dep.Wire {
				if !known[ecosystem] {
					continue
				}
				variant := detected[ecosystem]
				tool := adapter.ToolFor(ecosystem, variant)
				byTool[tool.Command] = tool
			}
		}
	}
	commands := make([]string, 0, len(byTool))
	for command := range byTool {
		commands = append(commands, command)
	}
	sort.Strings(commands)
	out := make([]adapter.ToolRequirement, 0, len(commands))
	for _, command := range commands {
		out = append(out, byTool[command])
	}
	return out, nil
}
