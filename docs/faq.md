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

## When should I use a submodule instead of copy vendoring?

Use a submodule when the source repository identity, compact history, and explicit gitlink are
useful; use copy when an ordinary clone must contain all files. Both remain fixed to the lock and
refuse dirty replacement without `--force`; see [Vendored dependencies](vendoring.md).

## Why does `fetch` exist when `update` already downloads data?

`fetch` is the lock-replay operation for a fresh clone: it restores disposable cache and vendored
trees without resolving a moving ref or changing durable files. `update` intentionally asks the
remote what a ref means now and may move the lock.

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

## What does `setup` write?

It writes repository-scoped skill pointers, one bounded AGENTS.md pointer, and supported harness
MCP configuration while preserving unrelated keys. It never changes home-directory configuration,
installs a harness, or grants MCP write/any-root access; preview it with `setup --dry-run`.

## Can it work without network access?

Yes, after the exact locked cache and required native dependencies are present. Run
`git-a2a fetch` while Git access is available, then `git-a2a status --offline`, `who`, `show`, and
`sync` use local files. CI should restore cache from the lock before going offline. Contact
delivery, moving-ref checks, and missing cache reconstruction naturally need their declared remote.

## Does git-a2a install package managers or run agents?

No. `doctor` reports required tools and installation hints, but never installs them. git-a2a
also does not run agent services, choose a chat platform, host cards, or retain message history.

## Why pin a JWKS if the card signature already verifies?

A `jku` inside the signed card proves only that the signer controls the key at that URL. An
attacker serving both a forged card and its JWKS can satisfy that circular claim. Pinning the
expected JWKS URL or RFC 7638 thumbprint supplies the consumer-controlled trust anchor; see the
[trust guide](trust.md).

## Why not operate an identity registry?

Repository owners already publish Git history, cards, origins, and keys through infrastructure
they control. git-a2a verifies consumer-selected anchors and records exact lock evidence; it does
not appoint a global authority or turn a discovery service into an identity provider.

## What happens to an unknown role, intent, contact kind, or ecosystem?

Those vocabularies are open. Unknown values are preserved and rendered; matching role/intent
tokens still route. An unknown contact kind has no delivery driver, and an unknown ecosystem has
no adapter, so the CLI reports the limitation instead of rejecting the manifest vocabulary.
