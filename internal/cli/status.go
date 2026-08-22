package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/neprel/git-a2a/adapters"
	"github.com/neprel/git-a2a/internal/cache"
	"github.com/neprel/git-a2a/internal/fetch"
	"github.com/neprel/git-a2a/internal/gitx"
	lockfile "github.com/neprel/git-a2a/internal/lock"
	"github.com/neprel/git-a2a/internal/manifest"
	"github.com/neprel/git-a2a/internal/render"
)

type statusRow struct {
	ID       string   `json:"id"`
	Upstream string   `json:"upstream"`
	Manifest string   `json:"manifest"`
	Wiring   string   `json:"wiring"`
	Agents   string   `json:"agents"`
	Sync     string   `json:"sync"`
	Details  []string `json:"details,omitempty"`
	failed   bool
}

func (a *App) status(args []string) int {
	offline, jsonOut, verbose := false, false, false
	wanted := map[string]bool{}
	for _, arg := range args {
		switch arg {
		case "--offline":
			offline = true
		case "--json":
			jsonOut = true
		case "-v":
			verbose = true
		default:
			if strings.HasPrefix(arg, "-") {
				fmt.Fprintf(a.Err, "status: unknown option %s\n", arg)
				return 2
			}
			wanted[arg] = true
		}
	}
	root := a.root()
	own, err := manifest.Load(filepath.Join(root, "a2amodule.yml"))
	if err != nil {
		fmt.Fprintf(a.Err, "status: no valid manifest: %v\n", err)
		return 2
	}
	l, err := lockfile.Load(root)
	if err != nil {
		fmt.Fprintf(a.Err, "status: %v\n", err)
		return 1
	}
	block, blockErr := render.Build(root, own, l, false)
	syncState := "current"
	if blockErr != nil || !render.Current(filepath.Join(root, "AGENTS.md"), block) {
		syncState = "stale"
	}
	var rows []statusRow
	matched := 0
	for _, dep := range own.Dependencies {
		if len(wanted) > 0 && !wanted[dep.ID] {
			continue
		}
		matched++
		entry, ok := l.Dependencies[dep.ID]
		row := statusRow{ID: dep.ID, Upstream: "unknown", Manifest: "unknown", Wiring: "clean", Agents: "unknown", Sync: syncState}
		if !ok {
			row.Manifest = "unlocked"
			row.failed = true
			rows = append(rows, row)
			continue
		}
		cached, loadErr := os.ReadFile(filepath.Join(cache.Dir(root, dep.ID), "a2amodule.yml"))
		if loadErr != nil {
			row.Manifest = "missing"
			row.failed = true
		} else {
			sum := sha256.Sum256(cached)
			actual := "sha256:" + hex.EncodeToString(sum[:])
			if actual != entry.Manifest {
				row.Manifest = "tampered"
				row.failed = true
				row.Details = append(row.Details, "cached manifest hash differs from lock")
			} else {
				row.Manifest = "clean"
			}
		}
		var depManifest *manifest.Manifest
		if len(cached) > 0 {
			depManifest, _ = manifest.Parse(cached)
		}
		if !offline {
			commit, e := gitx.Resolve(context.Background(), a.runner(), dep.Git, dep.Ref)
			if e != nil {
				row.Upstream = "unreachable"
				row.failed = true
				row.Details = append(row.Details, e.Error())
			} else if commit != entry.Commit {
				row.Upstream = "behind " + short(commit)
				row.failed = true
			} else {
				row.Upstream = "up to date"
			}
			tmp, e := os.MkdirTemp("", "git-a2a-status-")
			if e == nil {
				remote, e := (fetch.Fetcher{Runner: a.runner()}).Fetch(context.Background(), dep.Git, entry.Commit, defaultPath(dep.Path), tmp)
				_ = os.RemoveAll(tmp)
				if e != nil {
					row.Manifest = "remote unreadable"
					row.failed = true
				} else {
					sum := sha256.Sum256(remote.Manifest)
					if "sha256:"+hex.EncodeToString(sum[:]) != entry.Manifest {
						row.Manifest = "remote differs"
						row.failed = true
					}
				}
			}
		}
		if depManifest != nil {
			findings, e := driftAll(context.Background(), root, dep, *depManifest, entry)
			if e != nil {
				row.Wiring = "error"
				row.failed = true
				row.Details = append(row.Details, e.Error())
			} else if len(findings) > 0 {
				row.Wiring = fmt.Sprintf("%d drift", len(findings))
				row.failed = true
				for _, f := range findings {
					row.Details = append(row.Details, fmt.Sprintf("%s %s: want %s, got %s", f.File, f.Entry, f.Want, f.Got))
				}
			}
			agentState, failed, details := checkAgents(depManifest, entry.Cards, filepath.Join(cache.Dir(root, dep.ID), "cards"), offline)
			row.Agents = agentState
			row.failed = row.failed || failed
			row.Details = append(row.Details, details...)
		}
		if row.Sync == "stale" {
			row.failed = true
		}
		rows = append(rows, row)
	}
	if len(wanted) > 0 && matched == 0 {
		fmt.Fprintln(a.Err, "status: no dependencies matched")
		return 2
	}
	if len(wanted) == 0 {
		state, failed, details := checkAgents(own, nil, root, offline)
		rows = append(rows, statusRow{ID: own.Module.ID, Upstream: "self", Manifest: "valid", Wiring: "none", Agents: state, Sync: syncState, Details: details, failed: failed || syncState == "stale"})
	}
	failures := 0
	for _, row := range rows {
		if row.failed {
			failures++
		}
	}
	if failures > 0 {
		fmt.Fprintf(a.Err, "%d module(s): %d unhealthy or drifted\n", len(rows), failures)
	} else {
		fmt.Fprintf(a.Err, "%d module(s): clean\n", len(rows))
	}
	if jsonOut {
		public := make([]statusRow, len(rows))
		copy(public, rows)
		b, _ := json.MarshalIndent(public, "", "  ")
		fmt.Fprintln(a.Out, string(b))
	} else {
		fmt.Fprintln(a.Out, "MODULE\tUPSTREAM\tMANIFEST\tWIRING\tAGENTS\tSYNC")
		for _, row := range rows {
			fmt.Fprintf(a.Out, "%s\t%s\t%s\t%s\t%s\t%s\n", row.ID, row.Upstream, row.Manifest, row.Wiring, row.Agents, row.Sync)
			if verbose {
				for _, detail := range row.Details {
					fmt.Fprintf(a.Out, "  - %s\n", detail)
				}
			}
		}
	}
	if failures > 0 {
		return 1
	}
	return 0
}

func driftAll(ctx context.Context, root string, dep manifest.Dependency, m manifest.Manifest, locked manifest.LockedDependency) ([]stringFinding, error) {
	wanted := map[string]bool{}
	if dep.Wire != nil {
		for _, eco := range *dep.Wire {
			wanted[eco] = true
		}
		if len(*dep.Wire) == 0 {
			return nil, nil
		}
	}
	var out []stringFinding
	for _, implementation := range adapters.All() {
		if dep.Wire != nil && !wanted[implementation.Ecosystem()] {
			continue
		}
		ok, _, err := implementation.Detect(root)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		for _, exp := range m.Module.Exports {
			if exp.Ecosystem != implementation.Ecosystem() {
				continue
			}
			findings, err := implementation.Drift(ctx, root, dep, exp, locked)
			if err != nil {
				return nil, err
			}
			for _, f := range findings {
				out = append(out, stringFinding{f.File, f.Entry, f.Want, f.Got})
			}
		}
	}
	return out, nil
}

type stringFinding struct{ File, Entry, Want, Got string }

func checkAgents(m *manifest.Manifest, expected map[string]string, base string, offline bool) (string, bool, []string) {
	count := 0
	down := 0
	changed := 0
	var details []string
	for _, agent := range m.Agents {
		if agent.Card == "" {
			continue
		}
		count++
		if offline {
			continue
		}
		b, err := readCard(agent.Card, base)
		if err != nil {
			down++
			details = append(details, fmt.Sprintf("agent %s down: %v", agent.Name, err))
			continue
		}
		var card struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(b, &card) != nil || card.Name != agent.Name {
			down++
			details = append(details, fmt.Sprintf("agent %s returned an invalid or mismatched card", agent.Name))
			continue
		}
		if want := expected[agent.Name]; want != "" {
			sum := sha256.Sum256(b)
			if "sha256:"+hex.EncodeToString(sum[:]) != want {
				changed++
				details = append(details, fmt.Sprintf("agent %s card changed since snapshot", agent.Name))
			}
		}
	}
	if count == 0 {
		return "none", false, nil
	}
	if offline {
		return "unknown", false, nil
	}
	if down > 0 {
		return fmt.Sprintf("%d down", down), true, details
	}
	if changed > 0 {
		return fmt.Sprintf("%d changed", changed), true, details
	}
	return fmt.Sprintf("%d up", count), false, details
}
func readCard(location, base string) ([]byte, error) {
	if strings.HasPrefix(location, "http://") || strings.HasPrefix(location, "https://") {
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(location)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	}
	return os.ReadFile(filepath.Join(base, filepath.FromSlash(location)))
}
