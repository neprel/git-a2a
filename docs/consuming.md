# Consuming a git-a2a module

This guide follows a dependency from first import through routine CI and owner contact. For exact
flags and exit classes, use the [CLI reference](cli.md); for manifest fields, use the generated
[manifest reference](manifest-reference.md).

## Add the module

Start in a repository with its own `a2amodule.yml`. Applications are modules too: give the
consumer an id, owner, routing policy, and any exports it offers to other repositories.

```sh
git-a2a init --example app --id acme-app
git-a2a add https://github.com/acme/lib-utils.git
```

`add` resolves one commit, stores it in `a2amodule.lock`, and wires every applicable detected
ecosystem to that same commit. Use `--wire npm,pypi`, `--no-wire`, or `--no-refresh` only when the
repository policy calls for those narrower operations. A missing package-manager executable is
reported with an install hint; git-a2a never installs a toolchain.

## Keep source in the consumer repository

Vendoring is opt-in and consumer-owned. Use a submodule when the Git relationship should remain
visible, or a verified copy when the consumer must contain ordinary files:

```sh
git-a2a add https://github.com/acme/lib-utils.git --vendor submodule
git-a2a set acme-lib-utils --vendor copy
git-a2a set acme-lib-utils --no-vendor
```

The default location is `deps/<id>`; override it with `--vendor-path`. Native package managers
then use local path dependencies, while CMake, Gradle, MSBuild, Maven, and Meson receive their
generated integration. Meson projects use `--vendor-path subprojects/<id>`. Dirty or drifted
vendored content blocks update, replacement, and removal unless `--force` explicitly authorizes
data loss. Commit `.gitmodules` and the gitlink for submodule mode, or the copied tree for copy
mode, together with the manifest, lock, and native wiring.
The full transport/build-system matrix and CI/Windows notes are in
[Vendored dependencies](vendoring.md).

## Restore disposable state after clone

`.git-a2a/` is ignored and recoverable. A fresh clone reconstructs its locked manifests and,
when requested, published surfaces without resolving moving refs or changing durable files:

```sh
git-a2a fetch
git-a2a fetch acme-lib-utils --surface
```

If `who`, `show`, `sync`, `status`, or the MCP roster resource says the cache is missing, run
`git-a2a fetch`. A hash mismatch fails rather than accepting bytes that disagree with the lock.

## Publish the owner roster

```sh
git-a2a sync
git-a2a status --offline
```

`sync` creates one bounded block in `AGENTS.md`. Human content outside the delimiters survives.
Once the block exists, dependency mutations refresh it as their last transactional step. An
absent block is `SYNC none`, not unhealthy; an existing different block is `stale`.

## Inspect and update

```sh
git-a2a show acme-lib-utils --surface
git-a2a status -v
git-a2a update --check
git-a2a update acme-lib-utils --review
```

`status` compares the manifest, lock, cache, native wiring, cards, trust, upstream ref, and roster.
`update --check` exits `1` when a tracked ref has advanced. That is useful information, not a
reason for CI to rewrite a dependency automatically. Review the reported change, then run the
mutating update deliberately.

For public or cross-organisation dependencies, require signed commits/cards and an origin binding
in `dependencies[].require`. The `signers` file is repository-relative Git `allowed_signers`
syntax. Add/update/set/fetch verify before publishing lock/cache state; `trust show` explains the
result, and a deliberate card-key rotation needs `update --accept-keys`. See [Trust](trust.md).

Change source or tracking explicitly:

```sh
git-a2a set acme-lib-utils --ref v1.1.0 --dry-run
git-a2a set acme-lib-utils --ref v1.1.0
git-a2a pin acme-lib-utils
git-a2a unpin acme-lib-utils --ref main
```

All supported ecosystems continue to use the one commit recorded in the lock. `set`, `pin`, and
`unpin` are transactional; a failed adapter or roster refresh restores prior durable bytes.

## Repair wiring

```sh
git-a2a wire acme-lib-utils
git-a2a wire acme-lib-utils --ecosystem npm --no-refresh
```

`wire` repairs the precisely owned native dependency entries from manifest and lock. It does not
select a different commit. An explicitly required ecosystem that cannot express the source fails;
under implicit wiring it is reported as `not wired`.

## Ask the owner

```sh
git-a2a who acme-lib-utils --intent question
git-a2a who acme-lib-utils --intent change --path src/client.ts
git-a2a contact acme-lib-utils --intent change --message request.md
```

Routing is intent → policy role → most specific matching agent scope → declared contacts in
order. `contact` delivers through A2A and GitHub, GitLab, or Gitea-family issues when a consumer
CLI or token is available; otherwise it prints an exact deep-link instruction. A dependency on
GitLab or Codeberg uses the same `add`, `fetch`, `status`, lock, and vendoring workflows as any
other Git remote—only issue delivery differs. Open kinds can use a consumer-installed
[contact plugin](contact-plugins.md). See the generated [contact-kind reference](contact-kinds.md). Delivery
history is not stored.
An owner with `accepts-external: false` refuses a consumer from another organisation. Only a
human CLI invocation can approve the exception with `--external-ok`; MCP cannot bypass it.

## Remove

```sh
git-a2a remove acme-lib-utils
```

The command removes the manifest/lock entry and owned ecosystem wiring, then refreshes an existing
roster. Use `--keep-wiring` only when the remaining native entry is intentionally human-owned.

## CI after a fresh checkout

Fetch exact locked content before offline checks. Keep moving-ref discovery informational so
another repository's legitimate progress does not make the consumer's build nondeterministic:

```yaml
- name: Restore git-a2a cache
  run: git-a2a fetch
- name: Verify locked state
  run: git-a2a status --offline
- name: Report available dependency updates
  continue-on-error: true
  run: git-a2a update --check
```

For submodule consumers, clone with `--recurse-submodules`; `git-a2a fetch` also initializes the
locked submodule when an ordinary clone omitted it. Then run the repository's native builds and
tests. Do not commit `.git-a2a/`; commit
`a2amodule.yml`, `a2amodule.lock`, the managed `AGENTS.md` block, and native manifest/lock files.

The public [`consumer-app`](https://github.com/neprel/git-a2a-demo-acme-app) repository is a
working npm, Python, and Go example of this sequence.
