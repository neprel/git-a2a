# a2amodule specification, schema 2

`a2amodule.yml` (or `a2amodule.yaml`, but never both) declares one Git component,
its optional responsible external agent, its platform exports, an optional published
surface, and its direct dependencies. `a2amodule.lock` records the result of successfully
applying those dependencies. Disposable local state belongs under `.git-a2a/`.

The normative source is [`_.hint`](./_.hint). The strict JSON Schemas are
[`a2amodule.schema.json`](./schema/a2amodule.schema.json) and
[`a2amodule.lock.schema.json`](./schema/a2amodule.lock.schema.json), published as
`https://git-a2a.com/schema/a2amodule.v2.json` and
`https://git-a2a.com/schema/a2amodule-lock.v2.json`. Unknown fields are invalid.

## Manifest

The only top-level fields are `schema`, `component`, `agent`, and `dependencies`.
`schema` is always `2`; `component.id` is the only other field required in a minimal
declaration. `init` may leave `agent` absent, but a repository used as a dependency must
declare `agent.card`.

`component.exports` describes platform-facing packages or build targets. Every export has
an `adapter` and `name`, plus optional repository-relative `path`. The optional `checksum`
is adapter-specific source metadata required by Zig; it is not a second revision selector.
`component.surface` is either a safe relative directory or exact `.`. It publishes tracked
content from the applied commit for reading; it is not an installation mechanism. No surface
means no published source access.

`agent.card` is either an HTTPS URL or a safe path relative to the component root. The card,
not this manifest, owns protocol interfaces, skills, and authentication. `agent.name` is only
a display name.

Each dependency `name` is a consumer-local alias. Identity is not inferred from an agent name
or repository basename. `git`, optional `ref` and `path`, and `bindings` declare how the direct
component is applied. A binding stores both its `adapter` family and concrete `variant`, plus
optional upstream `export` and local `path`; pull reuses these selections instead of detecting
them again. Dependencies are direct only.

## Lock

The lock is CLI-owned deterministic YAML with `schema: 2` and a `dependencies` map keyed by
local alias. Every entry records upstream `component`, `git`, requested `ref`, optional `path`,
one resolved `commit`, the upstream `manifest` digest, the successfully applied `bindings`, and
the locked `agent`. Agent metadata separates `declaredCard` from the usable `card` reference and
records its `commit` provenance. Optional `surface` is the materialized Git tree identity. All
bindings, the manifest, agent metadata, and surface in one entry come from the same commit.

The declaration chooses source and adapters; the lock reports the last successful local
application. It is written only after the per-dependency transaction succeeds. It contains no
timestamps, machine-absolute paths, parsed Agent Card fields, or transitive package graph. A
repository-relative card is retained byte-for-byte under `.git-a2a/agents/ALIAS` as recoverable
local metadata; HTTPS references remain external and are not fetched.

## Examples

- [`native`](./examples/native.a2amodule.yml) — native Go integration.
- [`polyglot`](./examples/polyglot.a2amodule.yml) — npm/pnpm, Python/uv, and Go bindings.
- [`submodule-only`](./examples/submodule-only.a2amodule.yml) — explicit fallback checkout.
- [`submodule-build`](./examples/submodule-build.a2amodule.yml) — one checkout composed with CMake.
- [`surface-directory`](./examples/surface-directory.a2amodule.yml) and
  [`surface-root`](./examples/surface-root.a2amodule.yml) — directory and `.` surfaces.
- [`same-basename-aliases`](./examples/same-basename-aliases.a2amodule.yml) — two `common.git`
  repositories kept distinct by aliases.
- [`native.a2amodule.lock`](./examples/native.a2amodule.lock) — successful resolution.

The only schema 1 example is the deliberately invalid
[`schema-1-migration`](./examples/invalid/schema-1-migration.a2amodule.yml). Schema 1 is rejected
before mutation and is never reinterpreted. There is no compatibility reader or migrate command;
follow [`docs/migration-v2.md`](../docs/migration-v2.md) to author a schema 2 declaration.

## Local CLI observation contract

`list [--json]` reports all direct dependency aliases; its JSON form is always an array. Owner
lookup is separate: `whose ALIAS [--json]` returns one dependency's responsible Agent Card
reference and locally available surface without network access, agent execution, A2A messaging,
or file mutation. `list ALIAS` is invalid. A no-name `pull` against a valid manifest with no
dependencies succeeds without invoking adapters or writing state; a named unknown alias fails.
