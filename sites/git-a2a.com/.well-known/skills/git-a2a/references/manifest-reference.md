# Schema 2 manifest reference

The repository root contains exactly one manifest spelling: `a2amodule.yml` or
`a2amodule.yaml`. Generators use `.yml`. A consumer lock is `a2amodule.lock`; recoverable local
state is under ignored `.git-a2a/`.

## Complete component example

```yaml
schema: 2
component:
  id: lib-utils
  description: Shared parsing and validation utilities.
  repository: https://github.com/acme/lib-utils.git
  exports:
    - adapter: npm
      name: "@acme/lib-utils"
      path: packages/js
    - adapter: pypi
      name: acme-lib-utils
      path: packages/python
  surface: docs/public
agent:
  name: lib-utils-owner
  card: https://agents.acme.example/lib-utils/.well-known/agent-card.json
dependencies:
  - name: parser-core
    git: https://github.com/acme/parser-core.git
    ref: main
    bindings:
      - adapter: cargo
        variant: cargo
        export: parser-core
```

## Top-level fields

| Field | Required | Meaning |
| --- | --- | --- |
| `schema` | yes | Must be the integer `2`. Schema 1 is rejected before mutation. |
| `component` | yes | This repository's component declaration. |
| `agent` | for published dependencies | The single responsible external agent. `init` may omit it temporarily. |
| `dependencies` | no | Direct consumer dependencies. There is no transitive solver. |

Unknown legacy ownership, routing, trust, settings, or contact fields are invalid; they are not
silently preserved.

## `component`

| Field | Required | Meaning |
| --- | --- | --- |
| `id` | yes | Stable upstream component identity. It is separate from consumer-local aliases. |
| `description` | no | Short human-readable purpose. |
| `repository` | no | Canonical source URL, used as component metadata. |
| `exports` | no | Platform integrations published by the component. |
| `surface` | no | Relative directory or `.` published for consumer reading. |

Each export has required `adapter` and `name`, plus optional relative `path` and `checksum`.
`adapter` identifies a registered platform adapter. `name` is the ecosystem package/module name.
`path` selects an export inside a monorepo. A checksum, when used by that adapter, describes the
declared export rather than a separately resolved revision.

`surface` is tracked content from the same commit applied by the adapters. It is materialized at
`.git-a2a/surfaces/DEPENDENCY_NAME`. Traversal, `.git`, untracked files, and symlink escape are
forbidden. An absent field means no surface is published; it does not expose the whole repository.

## `agent`

| Field | Required | Meaning |
| --- | --- | --- |
| `card` | yes | HTTPS URL or safe repository-relative path of the responsible agent's A2A Agent Card. |
| `name` | no | Display name for local output. |

The Agent Card is authoritative for interfaces, skills, authentication, and other A2A metadata.
An HTTPS card stays an external reference and is not fetched to prove liveness. A relative card
is read from the same applied Git commit, relative to the upstream manifest root, and stored
byte-for-byte as recoverable consumer metadata.

## `dependencies[]`

| Field | Required | Meaning |
| --- | --- | --- |
| `name` | yes | Consumer-local alias and key. |
| `git` | yes | Upstream Git source. |
| `ref` | no | Requested branch, tag, or commit; omitted input is resolved to the real default branch by `add`. |
| `path` | no | Relative path to the component within the source repository. |
| `bindings` | after add | Adapter selections persisted for later pull/remove/inspect. |

Each binding contains required `adapter` and `variant`, plus optional `export` and `path`.
Selection happens on `add`; `pull` must reuse it or fail with an actionable mismatch. Multiple
bindings for one dependency all consume the same locked commit.

## Lock file

`a2amodule.lock` is generated state, not an authoring surface. For every alias it records the
upstream component, Git source, requested ref, optional path, exact commit, manifest identity,
bindings, agent metadata, and optional surface. Locked agent metadata contains the upstream
`declaredCard`, the usable HTTPS or consumer-local `card`, and the applied `commit` provenance.
A successful lock entry is never written before all owned mutations for that dependency succeed.
