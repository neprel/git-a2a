# Frequently asked questions

## Is git-a2a an A2A client?

No. It records the responsible agent's Agent Card reference and exposes it through `list`. An
external A2A client handles discovery details, authentication, messages, and tasks.

## Does it install any Git repository?

No. `add` requires an upstream schema 2 manifest with `agent.card`. A component declares how it
can be connected. When no native or build-system integration applies, git-a2a can use its
ordinary submodule adapter.

## What is a surface?

An optional, deliberately published set of tracked files from the applied component commit. It
may be documentation, API definitions, examples, or source. It is not an installation method or
permission to inspect other repository content.

## Is `pull` the same as `git pull`?

No. It resolves a dependency ref and asks that dependency's saved adapters to apply the commit.
It may run a native package manager or update a submodule, depending on the persisted binding.

## Can the package manager change automatically?

No. Add persists both adapter and variant. Pull fails if the saved variant is unavailable or no
longer valid rather than silently choosing another installed tool.

## Are transitive dependencies imported as git-a2a components?

No. The manifest contains direct dependencies only. Native package managers may still manage
their ordinary transitive packages, but git-a2a does not recursively import their agents or
surfaces.

## Can one repository declare multiple owner agents?

No. There is at most one responsible external agent. Internal delegation belongs to that agent
and is not part of the component contract.

## What happens after a fresh clone?

Run `git a2a pull`. It reconstructs missing owned materialization and surfaces while preserving
the saved adapters and variants.

## Is schema 1 supported?

No. Schema 1 is rejected before mutation. There is no compatibility reader, alias, or automatic
migration command. See [Migrating from schema 1](migration-v2.md).
