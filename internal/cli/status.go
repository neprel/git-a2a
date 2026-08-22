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
	"github.com/neprel/git-a2a/internal/a2a"
	"github.com/neprel/git-a2a/internal/cache"
	"github.com/neprel/git-a2a/internal/fetch"
	"github.com/neprel/git-a2a/internal/gitx"
	lockfile "github.com/neprel/git-a2a/internal/lock"
	"github.com/neprel/git-a2a/internal/manifest"
	"github.com/neprel/git-a2a/internal/render"
)

type statusRow struct {
	ID       string   `json:"id"`
	Source   string   `json:"source"`
	Ref      string   `json:"ref"`
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
		row := statusRow{ID: dep.ID, Source: "canonical", Ref: refLabel(dep.Ref, ""), Upstream: "unknown", Manifest: "unknown", Wiring: "clean", Agents: "unknown", Sync: syncState}
		if !ok {
			row.Manifest = "unlocked"
			row.failed = true
			rows = append(rows, row)
			continue
		}
		if entry.Git != dep.Git || entry.Ref != dep.Ref || entry.Path != defaultPath(dep.Path) {
			row.failed = true
			row.Details = append(row.Details, "manifest entry differs from lock — run update")
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
			if depManifest != nil {
				if depManifest.Module.MovedTo != nil {
					row.Source = "moved → " + depManifest.Module.MovedTo.Git
					row.failed = true
				} else if depManifest.Module.Repository != "" && gitx.NormalizeURL(dep.Git) != gitx.NormalizeURL(depManifest.Module.Repository) {
					row.Source = "fork of " + depManifest.Module.Repository
				}
			}
		}
		if !offline {
			resolution, e := gitx.ResolveDetailed(a.context(), a.runner(), dep.Git, dep.Ref)
			if e != nil {
				row.Upstream = "unreachable"
				row.failed = true
				row.Details = append(row.Details, e.Error())
			} else if resolution.Commit != entry.Commit {
				row.Upstream = "behind " + short(resolution.Commit)
				row.failed = true
			} else {
				row.Upstream = "up to date"
			}
			if e == nil {
				row.Ref = refLabel(dep.Ref, resolution.Kind)
			}
			work := cache.Dir(root, dep.ID)
			if e := os.MkdirAll(work, 0o755); e == nil {
				remote, e := (fetch.Fetcher{Runner: a.runner()}).Fetch(a.context(), dep.Git, resolution.Commit, defaultPath(dep.Path), work)
				if e != nil {
					row.Manifest = "remote unreadable"
					row.failed = true
				} else {
					if remoteManifest, parseErr := manifest.Parse(remote.Manifest); parseErr == nil {
						if remoteManifest.Module.MovedTo != nil {
							row.Source = "moved → " + remoteManifest.Module.MovedTo.Git
							row.failed = true
						} else if remoteManifest.Module.Repository != "" && gitx.NormalizeURL(dep.Git) != gitx.NormalizeURL(remoteManifest.Module.Repository) {
							row.Source = "fork of " + remoteManifest.Module.Repository
						}
					}
					sum := sha256.Sum256(remote.Manifest)
					if "sha256:"+hex.EncodeToString(sum[:]) != entry.Manifest {
						row.Manifest = "remote differs"
						row.failed = true
					}
				}
			}
		}
		if depManifest != nil {
			findings, wiringStates, e := driftAll(a.context(), root, dep, *depManifest, entry)
			if e != nil {
				row.Wiring = "error"
				row.failed = true
				row.Details = append(row.Details, e.Error())
			} else {
				if len(wiringStates) == 0 {
					row.Wiring = "none"
				} else {
					row.Wiring = strings.Join(wiringStates, ", ")
				}
				if len(findings) > 0 {
					row.failed = true
					for _, f := range findings {
						row.Details = append(row.Details, fmt.Sprintf("%s %s: want %s, got %s", f.File, f.Entry, f.Want, f.Got))
					}
				}
				if verbose {
					for _, state := range wiringStates {
						ecosystem, _, _ := strings.Cut(state, " ")
						if adapters.Verification(ecosystem) == "form-verified" {
							row.Details = append(row.Details, ecosystem+": form-verified (real toolchain integration pending)")
						}
					}
				}
			}
			agentState, failed, details := checkAgents(depManifest, entry.Cards, filepath.Join(cache.Dir(root, dep.ID), "cards"), root, offline)
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
		state, failed, details := checkAgents(own, nil, root, root, offline)
		rows = append(rows, statusRow{ID: own.Module.ID, Source: "self", Ref: "self", Upstream: "self", Manifest: "valid", Wiring: "none", Agents: state, Sync: syncState, Details: details, failed: failed || syncState == "stale"})
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
		fmt.Fprintln(a.Out, "MODULE\tSOURCE\tREF\tUPSTREAM\tMANIFEST\tWIRING\tAGENTS\tSYNC")
		for _, row := range rows {
			fmt.Fprintf(a.Out, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", row.ID, row.Source, row.Ref, row.Upstream, row.Manifest, row.Wiring, row.Agents, row.Sync)
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

func refLabel(ref, kind string) string {
	if len(ref) == 40 && isHex(ref) {
		return "pinned " + short(ref)
	}
	if kind == "tag" || strings.HasPrefix(ref, "refs/tags/") {
		return "tag " + strings.TrimPrefix(ref, "refs/tags/")
	}
	if kind == "branch" || strings.HasPrefix(ref, "refs/heads/") {
		return "branch " + strings.TrimPrefix(ref, "refs/heads/")
	}
	return "branch " + ref
}

func driftAll(ctx context.Context, root string, dep manifest.Dependency, m manifest.Manifest, locked manifest.LockedDependency) ([]stringFinding, []string, error) {
	wanted := map[string]bool{}
	if dep.Wire != nil {
		for _, eco := range *dep.Wire {
			wanted[eco] = true
		}
		if len(*dep.Wire) == 0 {
			return nil, nil, nil
		}
	}
	var out []stringFinding
	var states []string
	for _, implementation := range adapters.All() {
		if dep.Wire != nil && !wanted[implementation.Ecosystem()] {
			continue
		}
		ok, _, err := implementation.Detect(root)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			continue
		}
		hasExport := false
		unwired := false
		drifted := false
		for _, exp := range m.Module.Exports {
			if exp.Ecosystem != implementation.Ecosystem() {
				continue
			}
			hasExport = true
			findings, err := implementation.Drift(ctx, root, dep, exp, locked)
			if err != nil {
				return nil, nil, err
			}
			for _, f := range findings {
				out = append(out, stringFinding{f.File, f.Entry, f.Want, f.Got})
				if strings.TrimSpace(f.Got) == "" {
					unwired = true
				} else {
					drifted = true
				}
			}
		}
		if hasExport {
			state := "clean"
			if drifted {
				state = "drift"
			} else if unwired {
				state = "unwired"
			}
			states = append(states, implementation.Ecosystem()+" "+state)
		}
	}
	return out, states, nil
}

type stringFinding struct{ File, Entry, Want, Got string }

func checkAgents(m *manifest.Manifest, expected map[string]string, base, trustRoot string, offline bool) (string, bool, []string) {
	count := 0
	down := 0
	untrusted := 0
	changed := 0
	var details []string
	for _, agent := range m.Agents {
		if agent.Card == "" {
			continue
		}
		count++
		requiresSignature := agent.Trust != nil && agent.Trust.Signatures
		if offline && !requiresSignature {
			continue
		}
		location := agent.Card
		readBase := base
		if expected != nil && (offline || (!strings.HasPrefix(location, "http://") && !strings.HasPrefix(location, "https://"))) {
			location = a2a.FileName(agent.Name)
			readBase = base
		}
		b, err := readCard(location, readBase)
		if err != nil {
			if requiresSignature {
				untrusted++
				details = append(details, fmt.Sprintf("agent %s signature unavailable: %v", agent.Name, err))
			} else {
				down++
				details = append(details, fmt.Sprintf("agent %s down: %v", agent.Name, err))
			}
			continue
		}
		card, parseErr := a2a.Parse(b)
		name, _ := card["name"].(string)
		if parseErr != nil || name != agent.Name {
			down++
			details = append(details, fmt.Sprintf("agent %s returned an invalid or mismatched card", agent.Name))
			continue
		}
		if requiresSignature {
			if _, verifyErr := a2a.VerifySignatures(b, a2a.VerifyOptions{CacheRoot: trustRoot, Offline: offline}); verifyErr != nil {
				untrusted++
				details = append(details, fmt.Sprintf("agent %s signature invalid: %v", agent.Name, verifyErr))
				continue
			}
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
	if untrusted > 0 {
		return fmt.Sprintf("%d untrusted", untrusted), true, details
	}
	if offline {
		return "unknown", false, details
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
