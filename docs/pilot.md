# Pilot A runbook

Pilot A exercises a real library and consumer pair without putting private repository names,
URLs, card addresses, or channel names into this repository. The library publishes npm, PyPI,
and Go exports; the consumer takes the same locked commit through all three ecosystems.

## Inputs

Use fresh repositories and an absolute workspace outside the git-a2a checkout. Required values:

```sh
export GITA2A_PILOT_DIR=/absolute/private/path/acme-pilot
export GITA2A_PILOT_LIB_URL=ssh://git@example.test/acme/lib-utils.git
export GITA2A_PILOT_CLI_URL=ssh://git@example.test/acme/app-cli.git
export GITA2A_PILOT_LIB_CARD=https://agents.example.test/lib/.well-known/agent-card.json
export GITA2A_PILOT_CLI_CARD=https://agents.example.test/cli/.well-known/agent-card.json
export GITA2A_PILOT_SPEC_CARD=https://agents.example.test/spec/.well-known/agent-card.json
export GITA2A_PILOT_SURFACE_FILE=/absolute/private/path/API.md
export GITA2A_BIN=/absolute/path/to/git-a2a
```

Optional variables select refs, working branch, module ids, and export names. Defaults use only
the `acme-*` examples: `GITA2A_PILOT_LIB_REF`, `GITA2A_PILOT_CLI_REF`,
`GITA2A_PILOT_BRANCH`, `GITA2A_PILOT_{LIB,CLI,SPEC}_ID`, and
`GITA2A_PILOT_{LIB,CLI}_{NPM,PYPI,GO}`.

Run `make pilot`. The harness refuses an existing workspace, clones both repositories, creates
local `git-a2a-pilot` branches, installs the owner-provided surface summary, initializes the
consumer, writes template manifests using the supplied cards, and validates both manifests. It
does not commit, push, open an issue, or create a PR.

## Complete the lifecycle

Review the generated library manifest and surface with its owner, then commit and publish only
the pilot branch. Run the exact commands printed by the harness in the consumer:

```sh
git-a2a add "$GITA2A_PILOT_LIB_URL#$GITA2A_PILOT_BRANCH"
git-a2a sync
git-a2a who acme-lib-utils --intent change
git-a2a status acme-lib-utils --offline
git-a2a status acme-lib-utils
```

Remove any superseded hand-written dependency map or refresh target. Run `yarn install`, `uv
sync`, and `GOPRIVATE=… go build ./...` when those manifests exist. Commit manifest, lock,
managed AGENTS.md block, ecosystem manifests, and ecosystem locks; do not commit caches or build
outputs.

Make one owner-approved change to the library's published surface or API and publish it on the
pilot branch. In the consumer run:

```sh
git-a2a update --check       # expected exit 1 and old → new commit output
git-a2a update --review
git-a2a sync
git-a2a status acme-lib-utils --offline
```

Repeat the three ecosystem builds. Offline status must be clean. Online status may report cards
down only when the declared endpoints are intentionally unreachable from the runner; record that
topology rather than weakening the check.

## Report back

Keep the report outside this repository when it contains private values. Include:

1. Both committed manifests and the consumer lock.
2. Actual output from add, sync and the resulting managed block, offline and online status,
   routing for intent `change`, update check/review, and all three ecosystem builds.
3. Every guess, prompt, failure, or spec gap, each classified as `fix`, `won't fix`, or `later`.
   Record only anonymized, `acme-*` open questions in `spec/_.hint`.
