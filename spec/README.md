# a2amodule specification, schema 1

An `a2amodule.yml` file describes a git repository—or one module directory in a
monorepo—as reusable code with explicit agent ownership. The adjacent
`a2amodule.lock` records the exact revision seen by a consumer. Local fetched data lives
under `.git-a2a/` and is never committed.

The normative source is [`_.hint`](./_.hint). The JSON Schemas in [`schema/`](./schema/)
are the machine-readable form. Complete manifests live in [`examples/`](./examples/).

## Manifest

Top-level keys use this canonical order: `schema`, `module`, `agents`, `policy`,
`dependencies`, then extension keys. `schema` is `1`. `module.id` is the only other
required field.

- `module` names and describes the module, its published `surface`, release channel,
  languages, documentation, and ecosystem exports. Ecosystem values use package-url
  type names but remain an open vocabulary.
- `agents` binds existing agents to roles and path scopes. A binding points to an A2A
  card; it does not duplicate the card's self-description. Ordered contacts say how the
  agent accepts each request intent.
- `policy.intents` maps an intent to a role. Consumer permissions are declared under
  `policy.consumers.may` and `may-not`.
- `dependencies` identifies other modules by git URL, ref, optional monorepo `path`,
  tracking mode, and optional ecosystems to wire.

Unknown vocabulary values—roles, intents, contact kinds, and ecosystems—are valid.
Unknown object keys are invalid unless they start with `x-`.

## Routing

Routing is deterministic: request intent → role from `policy.intents` (default
`owner`) → the most specifically scoped matching agent → matching contacts in declared
order. An exact intent is preferred to the `"*"` fallback. The CLI reports the declared
contacts; it does not choose a fleet or communication platform.

## Lock and local state

The lock is CLI-owned deterministic YAML. Each dependency entry records its original
git URL, ref, module path, one resolved 40-hex commit, the SHA-256 of the fetched
manifest, optional card hashes, and—when available—the surface tree object. Every
ecosystem adapter pins the same commit. There are no timestamps or machine paths.

`.git-a2a/cache/<id>/` contains the fetched manifest, card snapshots, optional published
surface, fetch metadata, and an implementation working area. It is disposable and must
be ignored by git.

## A2A projection

Agent cards remain native A2A v1.0. `git-a2a card export` starts from a snapshotted card
or synthesises a minimal card from an A2A contact, then adds the stable extension
`https://git-a2a.com/ext/module/v1`. Its params bind the card to the module, repository,
role, scope, and ref. Skills and other native card fields are left untouched.

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
