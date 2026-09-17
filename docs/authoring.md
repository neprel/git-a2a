# Authoring a component

A publishable component is a Git repository with schema 2 `a2amodule.yml`, one responsible
external agent, and optional exports and surface.

## Start the declaration

```sh
git a2a init
```

`init` creates a minimal manifest and does not overwrite an existing `.yml` or `.yaml` spelling.
Edit the generated `component.id` and description, then add the Agent Card reference:

```yaml
schema: 2
component:
  id: lib-utils
  description: Shared parsing and validation utilities.
agent:
  name: lib-utils-owner
  card: https://agents.acme.example/lib-utils/.well-known/agent-card.json
```

`agent.card` is required before another repository can add the component. The URL is a contact
reference, not a liveness guarantee. Keep interfaces, skills, authentication, and descriptions
owned by the Agent Card rather than duplicating them in this manifest.

## Declare code integrations

Add one export for each supported consumer integration:

```yaml
component:
  id: lib-utils
  exports:
    - adapter: npm
      name: "@acme/lib-utils"
      path: packages/js
    - adapter: cargo
      name: lib-utils
      path: packages/rust
```

All exports must describe the same repository commit. For a monorepo, use relative `path` fields;
do not create separate ownership manifests in subdirectories. Build-system exports use the shared
submodule checkout selected by the consumer.

## Publish a readable surface

Set `component.surface` to a tracked relative directory, or `.` to publish all tracked repository
content except Git metadata:

```yaml
component:
  id: lib-utils
  surface: docs/public
```

Good surfaces contain stable API documentation, examples, schemas, or deliberately published
source. Surface is optional and is not how code is installed. Its bytes come from the exact
commit used by every adapter, so update it in the same commit as the behavior it documents.

## Before consumers add it

- Commit the schema 2 manifest at the selected ref.
- Ensure `agent.card` is present and points to the responsible repository agent.
- Ensure every export name and path matches the repository at that commit.
- Keep the surface tracked, bounded, and free of symlink escapes.
- Use `git a2a list --json` to inspect local declarations and problems.

Schema 1 fields are not accepted. Follow [the migration guide](migration-v2.md) for a manual
rewrite; there is no automatic migration command.
