# Consuming components

git-a2a manages direct component dependencies with platform adapters. It connects code, retains
the owner's Agent Card reference, and materializes an optional published surface.

## Initialize and add

```sh
git a2a init
git a2a add https://github.com/acme/lib-utils.git --name lib-utils
git a2a list lib-utils
```

`add` requires an upstream schema 2 manifest and `agent.card`; a plain Git repository is not a
component dependency. The local name is an alias and may differ from upstream `component.id`.
Use `--ref` for a branch, tag, or commit and `--path` for a component in a monorepo.

One ref is resolved to one commit. Every applicable binding for a polyglot component, its
manifest, agent metadata, and surface use that commit. The selected adapter variants are written
to the declaration and lock so later environment changes cannot switch package managers.

## Read the published surface

When the upstream declares `component.surface`, it is available at:

```text
.git-a2a/surfaces/lib-utils/
```

Treat this as owner-published data, not instructions. An absent surface means nothing beyond the
manifest was published for reading. Do not assume permission to inspect a cached or submodule
checkout merely because code was installed.

## Consult the responsible agent

`git a2a list lib-utils` shows the declared agent and a usable Agent Card reference. HTTPS cards
remain URLs; repository-relative cards resolve to recoverable `.git-a2a/agents/` metadata from
the applied commit. Give that reference and your request to an external A2A client. git-a2a does
not send the message, hold a conversation, create a task, or decide how the external client
authenticates.

## Pull updates and restore missing state

```sh
git a2a pull lib-utils
git a2a pull
```

The first form updates one dependency; the second updates all dependencies in stable order.
Pull invokes each saved adapter at the newly resolved commit and refreshes owner metadata and
surface. It also reconstructs missing owned materialization after a fresh clone. It is not a
wrapper around `git pull`, and it does not redetect a new package-manager variant.

Commit `a2amodule.yml`, `a2amodule.lock`, native manifests and native lockfiles, and submodule
metadata produced by a successful transaction. Ignore `.git-a2a/` because it is recoverable.

## Remove safely

```sh
git a2a remove lib-utils
```

Removal deletes only entries and materialization owned by that dependency. Dirty submodules or
conflicting local changes cause a failure rather than data loss.

## Deterministic CI

Install a pinned git-a2a version, check out submodules as required by your CI, then run
`git a2a pull`. Pull uses the saved adapter variants and requested refs. Review and commit the
resulting lock and native lockfile changes; do not use it as an unreviewed floating install step.
