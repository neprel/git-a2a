# git-a2a command reference

Every command accepts the global `--timeout DURATION` option (default `120s`) and `--yes` as a
non-interactive no-op for automation. No command prompts for input. Requested data is written to
stdout; verdicts and advisories go to stderr. Exit `0` means success, `1` means a
completed check found drift/failure, and `2` means invalid input or nothing resolved.
The repository's `.hint` sources and the commands used to read them are explained in
[Specification as source (HINT)](../README.md#specification-as-source-hint).

## init

`git-a2a init [--id ID] [--description TEXT] [--surface DIR] [--export ECOSYSTEM=NAME]
[--example lib|app] [--yes]`
creates `a2amodule.yml` and adds `.git-a2a/` to `.gitignore`. Repeat `--export`; `--yes` is an
accepted no-op for automation. `--example lib|app` writes a complete, commented owner or
consumer manifest; it may be combined with `--id`, but not the other content flags. Exit `1` if
a manifest already exists; invalid combinations exit `2`.

```text
$ git-a2a init --id acme-app --yes
initialized module acme-app
```

```text
$ git-a2a init --example lib --id acme-lib
initialized lib example module acme-lib
```

## validate

`git-a2a validate [FILE ...] [--json]` validates manifests and locks; without paths it checks the
files in the current module. `--json` emits one structured result per requested file, including
validation errors. Invalid files exit `1`; an empty subject set exits `2`.

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

## fetch

`git-a2a fetch [ID ...] [--surface] [--json]` restores disposable
`.git-a2a/cache` content from the exact commits and hashes in `a2amodule.lock`. Without IDs it
fetches every dependency; `--surface` also restores a declared surface whose tree hash is already
recorded in the lock. It never resolves a moving ref and never changes the manifest, lock, or
package-manager files. Missing/incomplete lock entries and hash mismatches exit `1`; invalid
options or an empty dependency set exit `2`.

```text
$ git-a2a fetch --json
[{"id":"acme-lib","commit":"ea1e8656ad1e6eaeef81759c10969e64defdd9ce","manifest":"sha256:…","method":"sparse"}]
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

## agent

`git-a2a agent add NAME --role ROLE [--scope GLOB]... [--card URL] [--contact FIELDS]...
[--yes]` adds an agent binding. Each contact is comma-separated `key=value`; list values such as
`intents` and `labels` use `|`, for example
`intents=question|change,kind=github-issue,repo=acme/lib,labels=from-agent|change-request`.
`git-a2a agent remove NAME [--yes]` removes it. `git-a2a agent list [--json] [--yes]` returns
agents in stable name order. Mutations validate and atomically write canonical YAML, then update
an existing AGENTS.md managed block. Invalid fields exit `2`; validation/write failures and
duplicates exit `1`; an unknown removal or empty list exits `2`.

```text
$ git-a2a agent add acme-lib-owner --role owner --scope '**' --contact 'intents=question|change,kind=github-issue,repo=acme/lib'
added agent acme-lib-owner
$ git-a2a agent list
acme-lib-owner  owner  **  1 contact(s)
1 agent(s)
```

## export

`git-a2a export add ECOSYSTEM NAME [--path PATH] [--yes]` adds a native export to the current
module. The result is validated and written atomically; relative path and duplicate violations
exit `1`, while invalid arguments exit `2`.

```text
$ git-a2a export add npm @acme/lib --path packages/js
added npm export @acme/lib
```

## policy

`git-a2a policy set INTENT=ROLE [INTENT=ROLE ...] [--yes]` creates or updates intent routing
without replacing consumer permissions, notes, extensions, or unrelated mappings. It writes
canonical YAML atomically. Invalid mappings exit `2`; validation/write failures exit `1`.

```text
$ git-a2a policy set question=owner change=spec
set 2 policy intent(s)
```

## explain

`git-a2a explain PATH [--json] [--yes]` prints the generated reference entry embedded in this
binary. Array markers may be omitted, so `agents.contacts.kind` resolves to
`agents[].contacts[].kind`. It performs no repository or network access. Unknown paths and
invalid arguments exit `2`.

```text
$ git-a2a explain module.id
## `module.id`
- Type: string; required.
…
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

## usage

`git-a2a usage [--prompt] [--json]` prints a deterministic briefing for coding agents. The
default is at most 60 lines and contains eight task commands with examples, exit-code meanings,
structured-output guidance, and the manifest-reference location. `--prompt` adds the full
fresh-agent workflow; `--json` emits the selected briefing as an ordered line array. Invalid
options exit `2`.

```text
$ git-a2a usage
git-a2a imports Git modules together with the agents that own them.
Read a2amodule.yml for the module contract and a2amodule.lock for exact resolved commits.
…
Exit 0: request completed or check clean.
```

## setup

`git-a2a setup [--check|--dry-run]` detects Claude Code, Codex, Cursor, GitHub Copilot,
Gemini CLI, and OpenCode from repository and user markers. It always installs the bundled skill
under `.agents/skills/git-a2a/`, also installs `.claude/skills/git-a2a/` when Claude Code is
present, and adds a bounded pointer block to `AGENTS.md`. For detected harnesses it writes only
the project-scoped `git-a2a` MCP entry in `.mcp.json`, `.codex/config.toml`,
`.cursor/mcp.json`, `.vscode/mcp.json`, `.gemini/settings.json`, or `opencode.json`, preserving
unrelated configuration. It never installs or upgrades the `git-a2a` executable.

`--dry-run` prints the files that would change and exits `0`; `--check` writes nothing and exits
`1` if any installed file or entry is missing/stale. An invalid existing config exits `1`; bad
options exit `2`.

```text
$ git-a2a setup --dry-run
would write .agents/skills/git-a2a/SKILL.md (cross-agent skill)
would write AGENTS.md (skill pointer)
setup: dry run; 5 file(s) would change
```

## version

`git-a2a version [--check]` prints version, commit, target, and install channel. `--check` alone
uses the network and exits `1` when an update is available. If only prereleases exist, it reports
that no stable release is published and exits `0`; prereleases never become `latest`.

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
