# git-a2a

[![built with HINT](https://img.shields.io/badge/built_with-HINT-5b4ee6)](https://openhint.dev/)
[![release](https://img.shields.io/github/v/release/neprel/git-a2a)](https://github.com/neprel/git-a2a/releases)
[![CI](https://github.com/neprel/git-a2a/actions/workflows/ci.yml/badge.svg)](https://github.com/neprel/git-a2a/actions/workflows/ci.yml)
[![npm](https://img.shields.io/npm/v/git-a2a)](https://www.npmjs.com/package/git-a2a)
[![PyPI](https://img.shields.io/pypi/v/git-a2a)](https://pypi.org/project/git-a2a/)
[![Homebrew](https://img.shields.io/badge/homebrew-neprel%2Ftap-fbb040)](https://github.com/neprel/homebrew-tap)
[![MCP Registry](https://img.shields.io/badge/MCP_Registry-io.github.neprel%2Fgit--a2a-5b4ee6)](https://registry.modelcontextprotocol.io/)

**Import a Git repository together with the agents that own it.**

git-a2a is an open standard plus a static Go CLI for micro-agent architectures. A repository
publishes one `a2amodule.yml`: what the module exports, which agents own it, how to contact them,
and which other modules it consumes. The CLI resolves one Git commit, wires every detected
ecosystem, maintains `a2amodule.lock`, and projects the useful ownership context into agent tools.

Read the [human specification](spec/README.md), the [command reference](docs/cli.md), or the
normative [`spec/_.hint`](spec/_.hint) with `hint spec`.

See the feature end to end in the public
[`acme-lib-utils`](https://github.com/neprel/git-a2a-demo-acme-lib) library and
[`consumer-app`](https://github.com/neprel/git-a2a-demo-acme-app) consumer repositories. The
[demo walkthrough](docs/demo.md) explains what to inspect and which commands to run.

## What you can do

| Capability | Commands and result |
| --- | --- |
| Dependency wiring | `add`, `wire`, `update`, `remove` edit native npm/Python/Go/Rust/Swift/Dart/Ruby/PHP/Elixir/Haskell/Zig/Clojure/Nix files at one resolved commit. |
| Source changes | `set`, `pin`, and `unpin` transactionally change URL, ref, monorepo path, tracking, or identity. |
| Owners and contacts | `who` routes an intent and path to the declared role, scoped agent, and ordered contacts. |
| Agent roster | `sync` maintains a bounded dependency/owner block in `AGENTS.md` or another target. |
| Contact delivery | `contact` sends through A2A or GitHub/GitLab/Gitea-family issues; any tracker can use exact instructions or a consumer plugin. |
| Vendoring and build systems | `--vendor submodule` or `--vendor copy` plus CMake/Gradle/MSBuild/Maven/Meson generated includes keep source at the lock commit. |
| Fresh-checkout restore | `fetch` reconstructs cache and vendored trees from the lock without resolving a new commit. |
| Cards, catalog, and trust | `card`, `catalog`, and `trust show` verify A2A cards, key/origin pins, signed commits, and ARD catalogs. |
| Liveness and drift | `status` compares upstream refs, manifest/cache hashes, native wiring, cards, trust, and synced context. |
| Agent UX | `usage`, the portable skill, `setup`, and `explain` brief and configure supported agent harnesses. |
| MCP | `mcp` exposes the same commands over bounded multi-repository stdio tools; write access is opt-in. |
| Prerequisites | `doctor` reports Git and native ecosystem tools with versions and install hints; it never installs them. |

## Quick start

```sh
git-a2a init --id acme-app
git-a2a add https://github.com/acme/lib-utils.git
git-a2a sync
git-a2a who acme-lib-utils --intent change
git-a2a status
git-a2a update --review
```

Owners start with `git-a2a init --example lib`, add an agent with `git-a2a agent add`, then run
`git-a2a validate` and `git-a2a card export`.

## Vendored dependencies

A consumer can materialize a locked dependency inside its repository with `add --vendor
submodule` or `add --vendor copy`; `set --vendor` changes that choice later. The lock still names
one Git commit for every ecosystem, while native npm, Cargo, Go, Pub, Mix, uv, and Composer wiring
and generated CMake, Gradle, MSBuild, Maven, or Meson integrations resolve through the vendored
directory. `git-a2a fetch` restores missing cache and vendored content from the lock in a fresh
checkout without resolving a new commit. See the [consumer workflow](docs/consuming.md#keep-source-in-the-consumer-repository)
and the live [`consumer-app`](https://github.com/neprel/git-a2a-demo-acme-app) submodule example.

## Works with

| Layer | Integrations and wiring |
| --- | --- |
| Native ecosystems | npm, uv/PyPI, Go, Cargo, SwiftPM, Pub, Bundler, Composer, Mix, Cabal/Stack, Zig, Clojure, Nix — native Git forms, or local path forms when vendored. |
| Build systems | CMake, Gradle, Maven, MSBuild, Meson — one generated include/import for vendored source. |
| Agent harnesses | Claude Code, Codex, Cursor, GitHub Copilot, Gemini CLI, OpenCode, Hermes Agent, OpenClaw. |
| Standards | A2A, AGENTS.md, Agent Skills, ARD catalogs, MCP (listed in the MCP Registry). |
| Issue delivery | GitHub, GitLab, Gitea, Forgejo, Codeberg, and any tracker through instructions or plugins. |

## Installation

Go-native channels are the simplest and come first. Every checkmark means the channel was
exercised against a public stable artifact on its native platform. These commands follow the
latest stable release; pin an explicit version in CI.

| Verified | Channel | Command |
| --- | --- | --- |
| ✔ | Go | `go install github.com/neprel/git-a2a/cmd/git-a2a@latest` |
| ✔ | Go zero-install | `go run github.com/neprel/git-a2a/cmd/git-a2a@latest --version` |
| ✔ | macOS/Linux installer | `curl -fsSL https://git-a2a.com/install.sh \| bash` |
| ✔ | Windows installer | `irm https://git-a2a.com/install.ps1 \| iex` |
| ✔ | Homebrew | `brew install neprel/tap/git-a2a` |
| ✔ | Scoop | `scoop bucket add git-a2a https://github.com/neprel/scoop-bucket; scoop install git-a2a` |
| ✔ | npm | `npx git-a2a@latest --version` |
| ✔ | PyPI with uv | `uvx git-a2a --version` |
| ✔ | PyPI with pipx | `pipx run git-a2a --version` |
| ✔ | Container | `docker run --pull=always --rm ghcr.io/neprel/git-a2a:latest --version` |

The checksum-verifying standalone installers support `GIT_A2A_VERSION`, `--version`, `--dir`,
and `--dry-run`. The macOS binaries are not yet Apple-notarized; use the Homebrew formula for a
checksum-verified first launch without a Gatekeeper quarantine prompt.

Linux `.deb`, `.rpm`, and `.apk` packages are attached to every GitHub Release:

```sh
sudo dpkg -i git-a2a_*.deb
sudo rpm -i git-a2a_*.rpm
sudo apk add --allow-untrusted git-a2a_*.apk
```

The scratch container contains only the binary:

```sh
docker run --pull=always --rm ghcr.io/neprel/git-a2a:latest version
```

Every channel executes the same Go binary. `git-a2a version --check` is the only automatic
release lookup and prints the correct manager-specific update command. `git-a2a upgrade` is
available only to the standalone binary channel; it never overwrites a package-manager install.
Release archives cover Darwin, Linux, and Windows on amd64/arm64 and include checksums and SBOMs.
Maintainer setup is in [docs/releasing.md](docs/releasing.md).

## Documentation

- [Manifest field reference](docs/manifest-reference.md): generated types, defaults, values, and consequences for every field.
- [Authoring guide](docs/authoring.md): create and publish a module, its surface, agents, contacts, policy, and cards.
- [Consumer guide](docs/consuming.md): add, fetch, sync, inspect, update, contact, and run deterministic CI.
- [Vendoring guide](docs/vendoring.md): submodule/copy tradeoffs, build systems, path mode, rollback, and CI.
- [Trust guide](docs/trust.md): pinned cards, signed commits, origins, rotation, and external delivery policy.
- [Agent/operator guide](docs/agents.md): usage, skill installation, setup by harness, MCP roots, and machine-output safety.
- [Contact kinds](docs/contact-kinds.md): generated allowed fields and delivery/instruction behavior for every known kind.
- [Contact plugins](docs/contact-plugins.md): consumer-side JSON protocol for open contact kinds.
- [FAQ](docs/faq.md): design boundaries, offline operation, A2A, MCP, Agent Skills, and disposable cache.
- [Consumer demo](docs/demo.md): inspect the public polyglot library and app end to end.
- [CLI reference](docs/cli.md): exact commands, flags, outputs, and exit codes.
- [MCP server](docs/mcp.md): attach the multi-repository stdio server to Claude Code, Codex,
  Cursor, Copilot, Gemini CLI, OpenCode, Hermes Agent, or OpenClaw.

## How it relates

- **A2A** is the agent-to-agent protocol. Agent Cards remain native A2A v1.0; the
  `https://git-a2a.com/ext/module/v1` extension binds a card to a Git module without copying its
  self-description. `contact` can deliver A2A `SendMessage` requests.
- **AGENTS.md** is a consumer-facing context surface. `sync` renders only published module,
  surface, ownership, and routing data into a managed block; it never imports dependency
  instructions or private implementation knowledge.
- **Agent Skills** teach a harness how to perform a task. They complement git-a2a: a Skill may
  call this CLI, while `a2amodule.yml` remains the durable, harness-neutral ownership and
  dependency contract shared by every agent environment. Install this repository's skill with
  `npx skills add neprel/git-a2a` or, before the 1.1.0 tag, `gh skill install neprel/git-a2a
  git-a2a --pin main`. From 1.1.0 onward the stable command is `gh skill install neprel/git-a2a
  git-a2a`; detailed references ship with the skill and in the npm launcher package. Run
  `git-a2a setup --dry-run` to preview repository-scoped skill, AGENTS.md, and MCP configuration
  for detected Claude Code, Codex, Cursor, Copilot, Gemini CLI, OpenCode, Hermes Agent, and
  OpenClaw environments.
- **MCP** is an optional stdio projection of the same CLI and files. It is not a daemon or an
  identity/package registry; declared roots bound which repositories tools may access.

git-a2a does not run agents, host endpoints, or choose a chat platform. Unknown contact kinds,
roles, intents, and ecosystems remain valid open vocabulary.

## Specification as source (HINT)

The repository's durable knowledge—decisions, invariants, and the normative standard in
[`spec/_.hint`](spec/_.hint)—lives in `.hint` files beside the artifacts it governs; this is
Spec-as-Source. `hint <path>` returns exactly the knowledge governing that path, `hint spec`
prints the standard, and `hint status` checks for drift. [HINT](https://openhint.dev/) is an
open, agent-neutral tool by the same author; install it with `npm install -g @openhint/cli` or
run it with `npx @openhint/cli`. This repository's vocabulary comes from
`hintbook-software-engineer`. Agents here run `hint` before editing; that requirement is the
purpose of the `<hint>` block in [`AGENTS.md`](AGENTS.md), while [`hint.yml`](hint.yml) selects
the hintbook.

License: MIT.

Built with [HINT](https://openhint.dev/).
