# a2amodule specification, schema 1

An `a2amodule.yml` file describes a git repository—or one module directory in a
monorepo—as reusable code with explicit agent ownership. The adjacent
`a2amodule.lock` records the exact revision seen by a consumer. Local fetched data lives
under `.git-a2a/` and is never committed.

The normative source is [`_.hint`](./_.hint). The JSON Schemas in [`schema/`](./schema/)
are the machine-readable form and are published at
[`/schema/a2amodule.v1.json`](https://git-a2a.com/schema/a2amodule.v1.json) and
[`/schema/a2amodule-lock.v1.json`](https://git-a2a.com/schema/a2amodule-lock.v1.json).
Complete manifests live in [`examples/`](./examples/).

## Manifest

Top-level keys use this canonical order: `schema`, `module`, `agents`, `policy`,
`dependencies`, then extension keys. `schema` is `1`. `module.id` is the only other
required field.

- `module` names and describes the module, its published `surface`, release channel,
  canonical `repository`, optional `moved-to` handoff, languages, documentation, and ecosystem
  exports. `repository` identifies the owner-declared source but is not fetched implicitly;
  `moved-to` is reported and followed only after explicit `update --follow-moves` or `set`.
  Ecosystem values use package-url type names but remain an open vocabulary.
- `agents` binds existing agents to roles and path scopes. A binding points to an A2A
  card; it does not duplicate the card's self-description. Ordered contacts say how the
  agent accepts each request intent.
- `policy.intents` maps an intent to a role. Consumer permissions are declared under
  `policy.consumers.may` and `may-not`.
- `dependencies` identifies other modules by git URL, ref, optional monorepo `path`, tracking
  mode, and optional ecosystems to wire. An absent `wire` means every detected applicable
  ecosystem; `wire: []` means none; a non-empty list makes exactly those adapters mandatory.
  Ecosystems that cannot express a source report `not wired` under implicit policy and fail the
  transaction when explicitly required.

Unknown vocabulary values—roles, intents, contact kinds, and ecosystems—are valid.
Unknown object keys are invalid unless they start with `x-`.

## Routing

Routing is deterministic: request intent → role from `policy.intents` (default
`owner`) → the most specifically scoped matching agent → matching contacts in declared
order. An exact intent is preferred to the `"*"` fallback. The CLI reports the declared
contacts; it does not choose a fleet or communication platform.

For example, with `policy.intents.change: spec`, a request for `change` under `api/**` first
selects agents whose role is `spec`, then prefers an `api/**` scope over `**`, and finally emits
that agent's `change` contacts in manifest order. If no exact contact accepts `change`, a
contact whose intents contain `"*"` is the fallback.

## Lock and local state

The lock is CLI-owned deterministic YAML. Each dependency entry records `git`, `ref`, `path`,
one resolved 40-hex `commit`, `manifest: sha256:<hex>`, optional `cards` hashes, and—when
materialised—`surface: tree:<40-hex>`. Every ecosystem adapter pins the same commit. There are
no timestamps or machine paths.

`.git-a2a/cache/<id>/` contains the fetched manifest, card snapshots, optional published
surface, fetch metadata, and an implementation working area. It is disposable and must
be ignored by git.

## A2A projection

Agent cards remain native A2A v1.0. `git-a2a card export` starts from a snapshotted card
or synthesises a minimal card from an A2A contact, then adds the stable extension
`https://git-a2a.com/ext/module/v1`. Its params bind the card to the module, repository,
role, scope, and ref. Skills and other native card fields are left untouched.

`agents[].trust.signatures: true` requires at least one valid JWS signature over the RFC 8785
canonical card. The protected header selects `alg`, `kid`, and an HTTPS `jku`; JWKS fetches are
timeout-bounded and cached under `.git-a2a/`. An unsigned, invalid, or unverifiable card makes
`status` unhealthy and produces an `update` warning. `git-a2a card verify FILE|URL` performs the
same verification explicitly.

`git-a2a catalog export [--out ai-catalog.json]` emits an ARD 1.0 catalog. Public card URLs stay
references; repository-relative or synthesised cards are embedded as
`application/a2a-agent-card+json`. The catalog indexes cards and never becomes another agent
description.

## Canonical YAML

CLI-owned YAML uses two-space indentation, the key order described above, block scalars
for multiline descriptions and notes, and block collections. A short contact may use a
flow map. Maps whose ordering is semantic (notably lock dependencies and card hashes)
are sorted lexicographically. Files end with one newline and contain no timestamps or
absolute machine paths.

Normal commands preserve a hand-written manifest's comments and key order outside the
precisely addressed entry they change. `git-a2a fmt` is the explicit request to rewrite
the whole manifest canonically.

See the [library](./examples/acme-lib-utils.a2amodule.yml),
[consumer](./examples/acme-app-cli.a2amodule.yml), and
[monorepo](./examples/acme-monorepo-consumer.a2amodule.yml) examples.
