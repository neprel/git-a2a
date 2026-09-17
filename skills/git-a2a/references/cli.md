# CLI reference

The executable may be invoked as `git-a2a` or, when installed on `PATH`, as `git a2a`.
The two spellings run the same program. The domain surface is exactly six commands.

## `init`

```text
git a2a init [--id ID] [--description TEXT]
```

Creates a minimal schema 2 `a2amodule.yml` in the repository root and adds `.git-a2a/` to
`.gitignore`. It refuses to overwrite either `a2amodule.yml` or `a2amodule.yaml`. The result may
omit `agent`, in which case the command reports that the component is incomplete for publication.
`--id` sets the component identity; `--description` sets its description.

## `add`

```text
git a2a add SOURCE [--name NAME] [--ref REF] [--path PATH]
```

Fetches the upstream schema 2 manifest, requires `agent.card`, rejects self-reference, and
resolves the requested ref once. If `--ref` is omitted, the remote's actual default branch is
recorded. `--name` selects the consumer-local alias; it need not equal `component.id`. `--path`
selects a component manifest in a subdirectory of the source repository.

The command chooses every applicable export/adapter for the consumer and persists each concrete
variant. Native managers install or resolve the dependency and reconcile their own locks. If no
integration applies or a native manager cannot represent the source, selection uses the submodule
adapter and reports any missing automatic build integration. Missing tools or network failures
do not trigger fallback. One transaction applies the code, agent metadata, optional surface,
declaration, and lock; the lock is written only after success.

## `pull`

```text
git a2a pull [NAME]
```

Without a name, updates every dependency in stable alias order. With a name, updates only that
dependency. Pull resolves each dependency's saved ref once, then calls its saved adapters and
variants at that commit. It also refreshes the upstream manifest, owner metadata, and optional
surface. Missing installed packages, target environments, cache, checkout, card, or surface
content is restored as part of this lifecycle, even when the resolved commit has not changed.
When the manifest has no dependencies, the no-name form prints `No dependencies.`, exits `0`,
and does not invoke adapters or write files. A named dependency that does not exist remains an
operational error.

`pull` is not the system `git pull` command. It never silently chooses another adapter or
package manager because the local environment changed. When updating all dependencies, a failure
may leave earlier successful dependencies updated; the summary identifies both successes and
failures.

## `remove`

```text
git a2a remove NAME
```

Removes the named dependency's owned native entries, submodule, surface, cache, declaration, and
lock entry. Native managers reconcile their locks and installed state, retaining packages still
needed by other dependencies; shared download caches need not be erased. It preserves
user-authored files, dirty submodules, unrelated native entries, and
other dependencies. Unsafe removal fails instead of deleting user changes.

## `list`

```text
git a2a list [--json]
```

Reports every direct dependency's alias, upstream component identity, Git source, requested ref,
installed commit, saved adapter variants, declared agent, usable Agent Card reference and
provenance, surface path, and local problems. `--json` returns an array, including `[]` when the
manifest has no dependencies. Positional arguments are invalid; use `whose NAME` for one owner.

This is an incompatible CLI change for the next release: scripts using `list NAME` must use
`list --json` and select the alias from the array when they need installed dependency state, or
use `whose NAME` when they need ownership metadata.

## `whose`

```text
git a2a whose NAME [--json]
```

Explains who is responsible for the dependency named by the required consumer-local alias. Text
output shows the upstream component, responsible agent, usable HTTPS Agent Card URL or local
`.git-a2a/agents/NAME/agent-card.json` path, and the local published surface when available.
Missing recoverable metadata is reported explicitly with a suggestion to run `pull NAME`.

`whose --json` returns one object with `name`, optional `component`, optional `agent`, optional
`surface`, and optional `problems`. The `agent` object is the existing locked Agent Card reference
and provenance (`name`, `declaredCard`, usable `card`, and `commit`); it does not duplicate or
interpret the Agent Card's interfaces, skills, authentication, or contacts.

Both `list` and `whose` are offline and read-only: they do not resolve remote refs, run package
managers or agents, send A2A messages, test liveness, or modify files.

## Service flags and exit status

`--help` and `--version` are service flags, not domain commands.

- Exit `0`: success.
- Exit `1`: operational or partial failure.
- Exit `2`: invalid invocation or input.

There are no compatibility aliases or migration command. See [Migrating from schema 1](migration-v2.md).
