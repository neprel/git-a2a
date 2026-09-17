# CLI reference

The executable may be invoked as `git-a2a` or, when installed on `PATH`, as `git a2a`.
The two spellings run the same program. The domain surface is exactly five commands.

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
git a2a list [NAME] [--json]
```

Reports the alias, upstream component identity, Git source, requested ref, installed commit,
saved adapter variants, declared agent, usable Agent Card reference and provenance, surface path,
and local problems. HTTPS cards remain URLs; repository-relative cards are available under
`.git-a2a/agents/NAME/`. It is
offline and read-only: it does not resolve a remote ref, run a package manager, or test agent
liveness. Missing recoverable local state is shown as unknown or missing with a suggestion to
run `pull`.

## Service flags and exit status

`--help` and `--version` are service flags, not domain commands.

- Exit `0`: success.
- Exit `1`: operational or partial failure.
- Exit `2`: invalid invocation or input.

There are no compatibility aliases or migration command. See [Migrating from schema 1](migration-v2.md).
