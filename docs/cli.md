# git-a2a command reference

Every command accepts the global `--timeout DURATION` option (default `120s`). Requested data is
written to stdout; verdicts and advisories go to stderr. Exit `0` means success, `1` means a
completed check found drift/failure, and `2` means invalid input or nothing resolved.

## init

`git-a2a init [--id ID] [--description TEXT] [--surface DIR] [--export ECOSYSTEM=NAME] [--yes]`
creates `a2amodule.yml` and adds `.git-a2a/` to `.gitignore`. Repeat `--export`; `--yes` is an
accepted no-op for automation. Exit `1` if a manifest already exists.

```text
$ git-a2a init --id acme-app --yes
initialized module acme-app
```

## validate

`git-a2a validate [FILE ...]` validates manifests and locks; without paths it checks the files
in the current module. Invalid files exit `1`; an empty subject set exits `2`.

```text
$ git-a2a validate
a2amodule.yml: valid
1 file(s): valid
```

## add

`git-a2a add URL [--id ID] [--path DIR] [--track locked|floating] [--wire LIST|--no-wire]
[--no-refresh]`
fetches the remote manifest, resolves one commit, wires detected ecosystems, writes the lock,
and snapshots cards. `--no-refresh` edits project manifests but skips package-manager Refresh.
Missing optional toolchains warn but do not prevent the manifest edit.
Exit `1` covers fetch/wiring failure and `2` invalid arguments.

```text
$ git-a2a add https://github.com/acme/lib.git --wire npm,golang
added acme-lib at ea1e8656ad1e6eaeef81759c10969e64defdd9ce
```

## set

`git-a2a set ID [--git URL] [--ref REF] [--path DIR] [--track locked|floating] [--id NEW-ID]
[--dry-run] [--no-refresh]` transactionally changes a dependency source or identity and rewires
it. `--no-refresh` skips package-manager Refresh. Exit `1`
means the transaction failed and rolled back; exit `2` means the ID/options did not resolve.

```text
$ git-a2a set acme-lib --ref release/1.x --dry-run
would set acme-lib to ref release/1.x
```

## pin

`git-a2a pin ID [COMMIT] [--no-refresh]` changes the dependency ref to a full 40-character
commit. Without `COMMIT`, the currently locked commit is used. `--no-refresh` skips
package-manager Refresh. Exit `1` means lock/rewiring failure; exit `2`
means an unknown ID or invalid SHA.

```text
$ git-a2a pin acme-lib
set acme-lib to https://github.com/acme/lib.git at ea1e8656ad1e6eaeef81759c10969e64defdd9ce
```

## unpin

`git-a2a unpin ID --ref REF [--track locked|floating] [--no-refresh]` returns a pinned dependency
to a branch or tag and resolves it immediately. `--no-refresh` skips package-manager Refresh.
Exit `1` means the transaction failed; exit `2` means the arguments or dependency were invalid.

```text
$ git-a2a unpin acme-lib --ref main
set acme-lib to https://github.com/acme/lib.git at ea1e8656ad1e6eaeef81759c10969e64defdd9ce
```

## wire

`git-a2a wire [ID] [--ecosystem NAME] [--no-refresh]` reapplies declared exports to detected
project files. With `--ecosystem`, that adapter is mandatory; `--no-refresh` skips its
package-manager Refresh. Invalid/missing subjects exit `2`; a required adapter failure exits `1`.

```text
$ git-a2a wire acme-lib --ecosystem npm
npm: wired acme-lib
```

## update

`git-a2a update [ID ...] [--check] [--review|--no-review] [--follow-moves] [--no-refresh]`
resolves upstream refs and transactionally updates changed dependencies. `--check` only reports
availability; `--review` prints manifest/surface diffs; `--no-refresh` skips package-manager
Refresh; moves require explicit `--follow-moves`. Exit `1`
means updates exist in check mode or an update failed; exit `2` means no dependency resolved.

```text
$ git-a2a update --check
acme-lib: ea1e8656ad1e -> 3ad806dc575c
1 dependency update(s) available
```

## remove

`git-a2a remove ID [--keep-wiring]` removes the manifest/lock/cache entry and normally unwires
all owned package-manager entries. Exit `1` means removal failed; exit `2` means the ID/options
did not resolve.

After any successful `add`, `update`, `set`, `pin`, `unpin`, `wire`, or `remove`, an existing
`AGENTS.md` managed block is rendered again as the final mutation. These commands never create a
new block; use `sync` once to opt in.

```text
$ git-a2a remove acme-lib
removed acme-lib (cache deleted; it can be recreated by add)
```

## show

`git-a2a show [ID] [--json] [--surface]` prints the own or cached dependency manifest. With
`--surface`, it materialises and lists the published surface before showing it. Exit `2` means
the module or surface was not resolvable.

```text
$ git-a2a show acme-lib --surface
surface/API.md
schema: 1
```

## sync

`git-a2a sync [--check] [--brief] [--target FILE]` renders the dependency/owner roster into
`AGENTS.md` and repeated targets. `--check` exits `1` without writing when blocks are stale.

```text
$ git-a2a sync
AGENTS.md
updated 1 managed block(s)
```

## who

`git-a2a who [ID] [--intent INTENT] [--path FILE] [--json]` applies intent → role → scoped
agent → contact routing. No match exits `2`.

```text
$ git-a2a who acme-lib --intent change
acme-lib change → owner → library-owner
```

## contact

`git-a2a contact ID --intent INTENT --message FILE|- [--wait]` uses the first supported routed
contact. A2A sends `SendMessage`; GitHub Issue uses `gh` then REST; URL/email/chat contacts print
instructions. Each delivery writes one record and stores no conversation state. `ask` is an
alias. Exit `1` means delivery failed; exit `2` means routing/input resolved nothing.

```text
$ printf 'Please review the API.' | git-a2a contact acme-lib --intent review --message -
acme-lib owner github-issue issue=https://github.com/acme/lib/issues/42
```

## status

`git-a2a status [ID ...] [--offline] [--json] [-v]` checks upstream, manifest/cache hashes,
wiring, cards/trust, and rendered blocks. The table contains dependencies only; the consuming
module is summarized below it. A repository that has not run `sync` has roster/SYNC `none`, which
is healthy; `stale` means an existing managed block differs. `-v` adds own-module findings,
prerequisite state, and adapter verification labels. Any unhealthy dependency or own-module
check exits `1`; no match exits `2`.

```text
$ git-a2a status --offline
acme-lib  canonical  branch main  unknown  clean  npm clean  unknown  none
consumer-app: manifest valid · agents none · roster none
1 dependency: clean
```

## card

`git-a2a card <export|validate|verify|show> [options]` manages native A2A cards:
`card export AGENT [--out FILE]`, `card validate FILE|URL`, `card verify FILE|URL`, and
`card show [ID] [AGENT] [--json]`. Unresolvable input exits `2`; invalid content/signature exits
`1`.

```text
$ git-a2a card verify ./owner-card.json
./owner-card.json: verified EdDSA signature with key production
card signature verified
```

## catalog

`git-a2a catalog export [--out FILE]` emits an ARD 1.0 `ai-catalog.json` whose entries reference
or embed the module's A2A cards. Exit `1` means encoding/writing failed; exit `2` means no valid
module or agents resolved.

```text
$ git-a2a catalog export --out ai-catalog.json
exported 2 A2A catalog entrie(s)
```

## fmt

`git-a2a fmt [--check] [PATH...]` canonicalises manifest/lock files or every matching file under
a supplied directory. `--check` exits `1` without writing when formatting differs.

```text
$ git-a2a fmt spec/examples
formatted 3 file(s)
```

## doctor

`git-a2a doctor [--json]` reports Git and every toolchain required by detected ecosystems and
wired dependencies, including version, PATH status, and platform installation hints. It never
installs anything. Missing required Refresh tools exit `1`.

```text
$ git-a2a doctor
git       2.51.0  found
npm       11.5.2  found
2 prerequisite(s): ready
```

## version

`git-a2a version [--check]` prints version, commit, target, and install channel. `--check` alone
uses the network and exits `1` when an update is available.

```text
$ git-a2a version
git-a2a 1.0.0 (2a46f1368876, darwin/arm64, channel=binary)
```

## upgrade

`git-a2a upgrade [--to VERSION]` downloads, checksum-verifies, and atomically replaces only a
standalone binary-channel installation. Managed channels exit `1` with their native update
command.

```text
$ git-a2a upgrade --to 1.0.1
upgraded git-a2a 1.0.0 -> 1.0.1
```
