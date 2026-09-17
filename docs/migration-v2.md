# Migrating from schema 1 to schema 2

Schema 2 is an intentional break. Current git-a2a rejects schema 1 before changing files. It has
no legacy reader, compatibility mode, deprecated aliases, or `migrate` command. Convert and
review the repository manually before using the five-command lifecycle.

## 1. Replace the ownership model

Replace `module` with `component`. Choose one responsible external agent and keep only its Agent
Card reference:

```yaml
schema: 2
component:
  id: lib-utils
  description: Shared parsing and validation utilities.
agent:
  name: lib-utils-owner
  card: https://agents.acme.example/lib-utils/.well-known/agent-card.json
```

Do not carry forward roles, scopes, intents, contacts, policies, trust pins, routing rules,
embedded interfaces, skills, authentication, liveness state, or multiple agents. The Agent Card
owns its A2A metadata. If there is no honest single responsible agent, leave `agent` absent while
authoring; consumers cannot add the component until an owner is declared.

## 2. Convert exports and surface

Move schema 1 module exports under `component.exports`. Rename each export's `ecosystem` to
`adapter`; preserve its ecosystem package name and relative path only when they still describe a
real adapter integration.

Move an intentional published directory to `component.surface`. Review it as a publication
boundary. Absence now means no source is published; do not invent `.` merely to preserve old
behavior.

## 3. Recreate consumer dependencies

Do not translate old lock entries, vendoring settings, floating state, or generated wiring.
Back up user-authored work, remove obsolete generated state, and use schema 2 `add` for each
direct dependency:

```sh
git a2a add https://github.com/acme/lib-utils.git --name lib-utils --ref main
```

This resolves a current commit, detects applicable adapters, records their concrete variants,
materializes the optional surface, and creates a fresh schema 2 lock. Review native manifests,
native lockfiles, `.gitmodules`, `a2amodule.yml`, and `a2amodule.lock` before committing.

## 4. Replace automation

Only these domain commands remain:

```text
init  add  pull  remove  list
```

Replace old restore or update jobs with `pull`; replace status/show/owner queries with local
`list` or `list --json`. Remove automation for message delivery, routing, card export, catalogs,
trust policy, copy-based vendoring, harness setup, MCP, and binary self-update. Use an external
A2A client to contact the card reported by `list`, and use the original installation channel to
update git-a2a itself.

## 5. Verify the cutover

- The root manifest says `schema: 2` and uses only `component`, optional `agent`, and optional
  direct `dependencies`.
- Every publishable upstream has exactly one `agent.card`.
- Every dependency was freshly added and has one commit shared by all bindings.
- Build-system integrations reference the shared submodule checkout; no copied vendor tree
  remains.
- CI and documentation invoke only `init`, `add`, `pull`, `remove`, `list`, `--help`, and
  `--version`.
