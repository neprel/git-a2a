# Frequently asked questions

## Why a repository manifest instead of a central registry?

The module contract changes with its code and owner. Keeping `a2amodule.yml` at the module root
makes one commit, review, signature policy, and Git history authoritative. Discovery sites may
index public modules, but git-a2a does not require or operate a registry service.

## Why fetch only the manifest and published surface?

A consumer needs the import contract, ownership routes, and content the owner deliberately
published. It does not need the dependency's implementation, private instructions, `.hint`, or
agent memory. For everything outside `module.surface`, the consumer asks the declared owner.

## Why must every ecosystem use one commit?

If npm follows one branch state while Python or Go follows another, a polyglot consumer no longer
has one dependency version. `a2amodule.lock` resolves the module once and every adapter derives its
native form from that commit. Floating mode may retain a native branch form, but the observed
commit remains recorded.

## Why is `.git-a2a/` disposable?

It contains fetched manifests, card snapshots, surfaces, trust keys, and Git working state. The
durable coordinates and hashes are in `a2amodule.lock`, so `git-a2a fetch` can reconstruct cache
content after a fresh clone. Committing the cache would duplicate derived state and machine details.

## How does this relate to A2A?

A2A defines native Agent Cards and agent-to-agent messaging. `a2amodule.yml` binds a repository
module to those cards without copying their self-description. `card export` adds the public
git-a2a module extension, and `contact` can deliver A2A `SendMessage` JSON-RPC requests.

## How does this relate to MCP?

`git-a2a mcp` is a thin stdio projection of the same CLI operations for tool-first harnesses.
It is not a daemon or alternative state model. One server can accept a repository `root` per
tool call, or a harness can start one independent server per repository. The shell/skill path
remains available and usually costs less context.

## How does this relate to Agent Skills and AGENTS.md?

An Agent Skill teaches a harness how to perform tasks. `git-a2a setup` installs a thin pointer
skill, while the full portable skill ships in this repository, npm, and the website. `AGENTS.md`
is the consumer-facing roster surface: `sync` owns only its delimited block. Neither replaces the
durable, harness-neutral manifest and lock.

## Can it work without network access?

Yes, after the exact locked cache and required native dependencies are present. Run
`git-a2a fetch` while Git access is available, then `git-a2a status --offline`, `who`, `show`, and
`sync` use local files. CI should restore cache from the lock before going offline. Contact
delivery, moving-ref checks, and missing cache reconstruction naturally need their declared remote.

## Does git-a2a install package managers or run agents?

No. `doctor` reports required tools and installation hints, but never installs them. git-a2a
also does not run agent services, choose a chat platform, host cards, or retain message history.

## What happens to an unknown role, intent, contact kind, or ecosystem?

Those vocabularies are open. Unknown values are preserved and rendered; matching role/intent
tokens still route. An unknown contact kind has no delivery driver, and an unknown ecosystem has
no adapter, so the CLI reports the limitation instead of rejecting the manifest vocabulary.
