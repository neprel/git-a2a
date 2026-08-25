package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/neprel/git-a2a/internal/a2a"
	"github.com/neprel/git-a2a/internal/cache"
	lockfile "github.com/neprel/git-a2a/internal/lock"
	"github.com/neprel/git-a2a/internal/manifest"
)

type trustShowResult struct {
	ID              string                            `json:"id"`
	Commit          string                            `json:"commit,omitempty"`
	CommitPolicy    string                            `json:"commitPolicy"`
	Verified        string                            `json:"verified,omitempty"`
	Signers         string                            `json:"signers,omitempty"`
	CardPolicy      string                            `json:"cardPolicy"`
	CardOrigin      bool                              `json:"cardOrigin"`
	CardKeys        map[string]manifest.LockedCardKey `json:"cardKeys,omitempty"`
	Origins         map[string][]string               `json:"origins,omitempty"`
	AcceptsExternal map[string]*bool                  `json:"acceptsExternal,omitempty"`
}

func (a *App) trust(args []string) int {
	if len(args) == 0 || args[0] != "show" {
		fmt.Fprintln(a.Err, "trust: expected show [ID] [--json]")
		return 2
	}
	jsonOut := false
	var ids []string
	for _, arg := range args[1:] {
		if arg == "--json" {
			jsonOut = true
		} else if len(arg) > 0 && arg[0] == '-' {
			fmt.Fprintf(a.Err, "trust show: unknown option %s\n", arg)
			return 2
		} else {
			ids = append(ids, arg)
		}
	}
	if len(ids) > 1 {
		fmt.Fprintln(a.Err, "trust show: at most one dependency id is allowed")
		return 2
	}
	root := a.root()
	own, err := manifest.LoadDir(root)
	if err != nil {
		fmt.Fprintf(a.Err, "trust show: %v\n", err)
		return 2
	}
	locked, err := lockfile.Load(root)
	if err != nil {
		fmt.Fprintf(a.Err, "trust show: %v\n", err)
		return 1
	}
	var results []trustShowResult
	for _, dependency := range own.Dependencies {
		if len(ids) == 1 && ids[0] != dependency.ID {
			continue
		}
		entry := locked.Dependencies[dependency.ID]
		result := trustShowResult{ID: dependency.ID, Commit: entry.Commit, CommitPolicy: "any", CardPolicy: "any", CardKeys: entry.CardsKeys}
		if dependency.Require != nil {
			if dependency.Require.Commits != "" {
				result.CommitPolicy = dependency.Require.Commits
			}
			result.Signers = dependency.Require.Signers
			if dependency.Require.Cards != "" {
				result.CardPolicy = dependency.Require.Cards
			}
			result.CardOrigin = dependency.Require.CardOrigin
		}
		result.Verified = entry.Verified
		if cached, loadErr := manifest.LoadDir(cache.Dir(root, dependency.ID)); loadErr == nil {
			for _, agent := range cached.Agents {
				if agent.Trust == nil {
					continue
				}
				if len(agent.Trust.Origins) > 0 {
					if result.Origins == nil {
						result.Origins = map[string][]string{}
					}
					result.Origins[agent.Name] = agent.Trust.Origins
				}
				if agent.Trust.AcceptsExternal != nil {
					if result.AcceptsExternal == nil {
						result.AcceptsExternal = map[string]*bool{}
					}
					result.AcceptsExternal[agent.Name] = agent.Trust.AcceptsExternal
				}
			}
		}
		results = append(results, result)
	}
	if len(ids) == 1 && len(results) == 0 {
		fmt.Fprintf(a.Err, "trust show: unknown dependency %s\n", ids[0])
		return 2
	}
	if jsonOut {
		body, _ := json.MarshalIndent(results, "", "  ")
		fmt.Fprintln(a.Out, string(body))
	} else {
		for _, result := range results {
			verified := result.Verified
			if verified == "" {
				verified = "unverified"
			}
			fmt.Fprintf(a.Out, "%s: commits %s (%s), cards %s, origin-required %t\n", result.ID, result.CommitPolicy, verified, result.CardPolicy, result.CardOrigin)
			names := make([]string, 0, len(result.CardKeys))
			for name := range result.CardKeys {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				key := result.CardKeys[name]
				fmt.Fprintf(a.Out, "  %s: key %s %s\n", name, key.KeyID, key.Thumbprint)
			}
		}
	}
	fmt.Fprintf(a.Err, "%d trust declaration(s)\n", len(results))
	return 0
}

func inspectCardTrust(m *manifest.Manifest, cardsDir, root, canonicalGit, commit string, require *manifest.Require) (map[string]manifest.LockedCardKey, []error) {
	keys := map[string]manifest.LockedCardKey{}
	var warnings []error
	for _, agent := range m.Agents {
		if agent.Card == "" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(cardsDir, a2a.FileName(agent.Name)))
		if err != nil {
			warnings = append(warnings, fmt.Errorf("%s: %w", agent.Name, err))
			continue
		}
		requiresSignature := agent.Trust != nil && agent.Trust.Signatures || require != nil && require.Cards == "signed"
		if requiresSignature {
			verification, verifyErr := a2a.VerifySignatures(raw, cardVerifyOptions(agent, root, false, agent.Card))
			if verifyErr != nil {
				warnings = append(warnings, fmt.Errorf("%s: %w", agent.Name, verifyErr))
			} else {
				keys[agent.Name] = manifest.LockedCardKey{
					KeyID: verification.KeyID, Thumbprint: verification.Thumbprint,
					JWKS: verification.JWKS, FirstSeen: commit,
				}
				if verification.UnpinnedKey {
					warnings = append(warnings, fmt.Errorf("%s: unpinned key source", agent.Name))
				}
			}
		}
		origins := []string(nil)
		if agent.Trust != nil {
			origins = agent.Trust.Origins
		}
		if originErr := a2a.CheckOrigins(raw, agent.Card, m.Module.Repository, canonicalGit, origins); originErr != nil {
			if requiresSignature || require != nil && require.CardOrigin {
				warnings = append(warnings, fmt.Errorf("%s: %w", agent.Name, originErr))
			}
		}
	}
	if len(keys) == 0 {
		keys = nil
	}
	return keys, warnings
}

func reconcileCardKeys(previous, current map[string]manifest.LockedCardKey, accept bool) (map[string]manifest.LockedCardKey, []error) {
	result := make(map[string]manifest.LockedCardKey, len(previous)+len(current))
	for name, key := range previous {
		result[name] = key
	}
	var warnings []error
	for name, key := range current {
		old, exists := previous[name]
		if !exists || accept {
			result[name] = key
			continue
		}
		if old.KeyID != key.KeyID || old.Thumbprint != key.Thumbprint {
			warnings = append(warnings, fmt.Errorf("%s: trust: key changed; run git-a2a update --accept-keys", name))
			continue
		}
		key.FirstSeen = old.FirstSeen
		result[name] = key
	}
	if len(result) == 0 {
		return nil, warnings
	}
	return result, warnings
}
