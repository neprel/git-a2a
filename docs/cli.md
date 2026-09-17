# CLI reference

The executable may be invoked as `git-a2a` or, when installed on `PATH`, as `git a2a`.
The two spellings run the same program. The domain surface is exactly five commands.

## `init`

```text
git a2a init
```

Creates a minimal schema 2 `a2amodule.yml` in the repository root and adds `.git-a2a/` to
`.gitignore`. It refuses to overwrite either `a2amodule.yml` or `a2amodule.yaml`. The result may
omit `agent`, in which case the command reports that the component is incomplete for publication.

## `add`

```text
git a2a add SOURCE [--name NAME] [--ref REF] [--path PATH]
```

Fetches the upstream schema 2 manifest, requires `agent.card`, rejects self-reference, and
resolves the requested ref once. If `--ref` is omitted, the remote's actual default branch is
recorded. `--name` selects the consumer-local alias; it need not equal `component.id`. `--path`
selects a component manifest in a subdirectory of the source repository.

The command chooses every applicable export/adapter for the consumer and persists each concrete
variant. If no native integration applies, it uses the submodule adapter. One transaction applies
the code, optional surface, declaration, and lock; the lock is written only after success.

## `pull`

```text
git a2a pull [NAME]
```

Without a name, updates every dependency in stable alias order. With a name, updates only that
dependency. Pull resolves each dependency's saved ref once, then calls its saved adapters and
variants at that commit. It also refreshes the upstream manifest, owner metadata, and optional
surface. Missing cache, checkout, or surface content is restored as part of this lifecycle.

`pull` is not the system `git pull` command. It never silently chooses another adapter or
package manager because the local environment changed. When updating all dependencies, a failure
may leave earlier successful dependencies updated; the summary identifies both successes and
failures.

## `remove`

```text
git a2a remove NAME
```

Removes the named dependency's owned native entries, submodule, surface, cache, declaration, and
lock entry. It preserves user-authored files, dirty submodules, unrelated native entries, and
other dependencies. Unsafe removal fails instead of deleting user changes.

## `list`

```text
git a2a list [NAME] [--json]
```

Reports the alias, upstream component identity, Git source, requested ref, installed commit,
saved adapter variants, declared agent and card URL, surface path, and local problems. It is
offline and read-only: it does not resolve a remote ref, run a package manager, or test agent
liveness. Missing recoverable local state is shown as unknown or missing with a suggestion to
run `pull`.

## Service flags and exit status

`--help` and `--version` are service flags, not domain commands.

- Exit `0`: success.
- Exit `1`: operational or partial failure.
- Exit `2`: invalid invocation or input.

There are no compatibility aliases or migration command. See [Migrating from schema 1](migration-v2.md).
