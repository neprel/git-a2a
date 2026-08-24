# Authoring an a2amodule

This guide is for the owner of a repository that other agents or projects should consume. The
[field reference](manifest-reference.md) is the exact lookup for types and defaults; this page is
the sequence for making a useful contract. The public
[`acme-lib-utils`](https://github.com/neprel/git-a2a-demo-acme-lib) repository is the worked
example throughout.

## 1. Start at the repository boundary

Run `init` in the repository root, or in the module directory inside a monorepo:

```sh
git-a2a init --id acme-lib-utils \
  --description "Shared formatting utilities. Ask about the cross-language contract." \
  --surface surface/
```

`module.id` is the stable consumer identity and lock key. Keep it short, lowercase, and stable;
renaming it breaks references. Add `module.repository` as the owner-declared canonical Git URL.
It is provenance, not an implicit fetch redirect. If the module later moves, leave
`module.moved-to` at the old source so `update` can report the destination without silently
following it.

Declare `module.release.channel` when consumers should follow a branch such as `main`; otherwise
an add with no ref follows the remote default branch. Set `release.tags: true` only when the
repository really publishes semantic-version tags. The declaration does not create releases.

## 2. Declare every importable export

An export says how the same Git revision appears to a native ecosystem. Its `name` is the value
that ecosystem imports, not necessarily the repository or module id.

| Ecosystem | `name` means | Typical native file |
| --- | --- | --- |
| `npm` | package name, including scope | `package.json` |
| `pypi` | PEP 503 distribution name | `pyproject.toml` |
| `golang` | Go module import path | `go.mod` |
| `cargo` | crate name | `Cargo.toml` |
| `swift` | SwiftPM package identity | `Package.swift` |
| `pub` | Dart package name | `pubspec.yaml` |
| `gem` | gem name | `Gemfile` |
| `composer` | Composer `vendor/package` | `composer.json` |
| `hex` | Hex package name | `mix.exs` |
| `hackage` | Cabal package name | `*.cabal` or `stack.yaml` |
| `zig` | dependency name | `build.zig.zon` |
| `clojure` | deps.edn symbol | `deps.edn` |
| `nix` | flake input name | `flake.nix` |

Use `path` when that package lives below the module directory. An ecosystem that cannot express
the Git URL, ref mode, or subdirectory is reported as `not wired`; never claim an import that the
native tool cannot reproduce. The demo library declares npm, PyPI, and Go exports for one API:

```yaml
exports:
  - { ecosystem: npm, name: "@acme/lib-utils" }
  - { ecosystem: pypi, name: acme-lib-utils }
  - { ecosystem: golang, name: github.com/neprel/git-a2a-demo-acme-lib }
```

## 3. Publish a surface, not the implementation

`module.surface` is the directory consumers may read in addition to the manifest. Put stable API
signatures, behavior, limits, compatibility notes, and small usage examples there. Do not publish
internal plans, private prompts, credentials, memories, or implementation instructions.

The boundary is ask-not-read: if an answer is not in the surface, the consumer contacts an owner.
The demo's `surface/API.md` states the shared function contract and `surface/NOTES.md` states
cross-language facts. `git-a2a show acme-lib-utils --surface` materializes that declared content
without granting access to the rest of the dependency repository.

## 4. Bind agents and cards

Each agent binding needs a stable `name` and a `role`; `scope` defaults to `**`. A more specific
matching scope wins before manifest order. Core roles are `owner`, `maintainer`, `spec`,
`reviewer`, and `support`, but roles remain an open vocabulary.

`card` may be a live A2A v1.0 URL or a repository-relative static JSON file. The card is the
agent's self-description; do not copy its skills, interfaces, or security into the manifest.
Use binding `description` only for repository-specific context or when no card exists. A static
card may demonstrate discovery without running an agent service, but its description must say so.

With `trust.signatures: true`, the card must carry a valid detached JWS over its RFC 8785
canonical form. Pin its JWKS URL in `trust.jwks` or its RFC 7638 thumbprint in `trust.keys`;
`trust.origins` can constrain card and interface origins, and `trust.jwks-max-age` controls key
refresh. `status` fails for unsigned or invalid cards; `update` retains code changes and prints a
trust warning. Use `git-a2a card verify FILE_OR_URL --jwks URL` before enabling the requirement.

Set `accepts-external: false` when an agent declines delivery from consumers outside the owner's
organisation. `settings.organisation` declares equivalent repository prefixes when the default
host/owner identity is insufficient. The complete threat model and key-rotation workflow are in
the [trust guide](trust.md).

## 5. Add contacts by intent

Contacts are ordered. Every contact declares one or more request `intents` and a `kind`. Core
intents are `question`, `change`, `bug`, `review`, and `incident`; both intents and kinds remain
open vocabularies. `"*"` is the fallback for an otherwise unmatched intent.

```yaml
agents:
  - name: acme-pm
    role: spec
    scope: [surface/**, a2amodule.yml]
    card: https://git-a2a.com/demo/agents/acme-pm/.well-known/agent-card.json
    contacts:
      - intents: [change]
        kind: github-issue
        repo: neprel/git-a2a-demo-acme-lib
        labels: [change-request, from-agent]
        note: Describe the consumer, the need, and the affected languages.
```

`a2a` and `github-issue` have delivery drivers. `url`, `email`, and chat kinds print an exact
instruction; they do not pretend delivery occurred. The kind-specific keys and delivery behavior
are listed in the [field reference](manifest-reference.md).

## 6. State routing and the consumer boundary

`policy.intents` maps an intent to a role. Unlisted intents route to `owner`. In the demo,
`change: spec` sends contract requests to `acme-pm`, while bugs stay with the owner.

`policy.consumers.may` and `may-not` are open-vocabulary declarations rendered into the managed
AGENTS.md roster. They tell an agent what the owner permits; git-a2a is not an authorization
service. A useful library boundary is:

```yaml
policy:
  intents: { change: spec }
  consumers:
    may: [read-surface, ask, open-issue, propose-change]
    may-not: [commit, edit-spec, release]
```

## 7. Validate and publish projections

Before committing, run:

```sh
git-a2a fmt a2amodule.yml
git-a2a validate a2amodule.yml
git-a2a card export acme-lib-utils --out acme-lib-utils.agent-card.json
git-a2a card validate acme-lib-utils.agent-card.json
git-a2a catalog export --out ai-catalog.json
git-a2a status -v
```

Review the formatted manifest and generated projections, then commit the manifest, surface, and
any intentionally static cards/catalog. Do not commit `.git-a2a/`; it is disposable local cache.
The owner publishes the Git ref and any card URLs through its normal release and hosting process.

## 8. What the consumer receives

A consumer runs `git-a2a add URL` and gets one resolved commit in `a2amodule.lock`, native package
entries for every applicable export, card snapshots in ignored local state, and the published
surface on demand. `git-a2a sync` writes a bounded roster into AGENTS.md containing the module
description, consumer policy, routed owners, and declared contacts. It never imports dependency
instructions or code.

Clone the [public consumer](https://github.com/neprel/git-a2a-demo-acme-app), then run `status`,
`who`, `show --surface`, and `update --check` to see the authored contract from the other side.
