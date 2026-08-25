<!-- Code generated from internal/cli MCP registration; DO NOT EDIT. -->
# MCP tool text audit

This table is generated from the same facts used to register `tools/list`. `make docs-check`
fails if the checked-in table differs. Default access exposes exactly eight tools;
`--allow-write` adds six tools.

| Access | Tool | Description | readOnly | destructive | idempotent | openWorld |
|---|---|---|---|---|---|---|
| `default` | `who` | Resolve an intent to the owning agents and their declared contacts. | yes | no | no | no |
| `default` | `show` | Read this module or a locked dependency and optionally its published surface. | yes | no | no | no |
| `default` | `status` | Check dependency, wiring, card, trust, and roster health. | yes | no | no | no |
| `default` | `validate` | Validate manifest and lock files against the git-a2a standard. | yes | no | no | no |
| `default` | `doctor` | Report Git and ecosystem tool prerequisites without installing anything. | yes | no | no | no |
| `default` | `fetch` | Restore disposable dependency cache content from exact lock coordinates. | no | no | yes | yes |
| `default` | `explain` | Read the normative generated reference entry for one manifest field. | yes | no | no | no |
| `default` | `usage` | Read the compact or full deterministic briefing for a coding agent. | yes | no | no | no |
| `--allow-write` | `add` | Import a Git module, resolve one commit, and wire declared ecosystems. | no | no | yes | no |
| `--allow-write` | `update` | Resolve tracked refs and transactionally update selected dependencies. | no | no | yes | no |
| `--allow-write` | `set` | Transactionally change a dependency source, ref, path, tracking, or id. | no | no | yes | no |
| `--allow-write` | `wire` | Repair native ecosystem dependency entries from manifest and lock. | no | no | yes | no |
| `--allow-write` | `sync` | Render or check the bounded git-a2a roster in instruction files. | no | no | yes | no |
| `--allow-write` | `contact` | Deliver a request through the owner's first supported declared contact. | no | no | no | yes |
