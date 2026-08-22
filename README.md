# git-a2a

[![built with HINT](https://img.shields.io/badge/built_with-HINT-5b4ee6)](https://openhint.dev/)

**Import a Git repository together with the agents that own it.**

git-a2a is an open standard plus a static Go CLI for micro-agent architectures. A repository
publishes one `a2amodule.yml`: what the module exports, which agents own it, how to contact them,
and which other modules it consumes. The CLI resolves one Git commit, wires every detected
ecosystem, maintains `a2amodule.lock`, and projects the useful ownership context into agent tools.

Status: **1.0.0 — first release.** Read the [human specification](spec/README.md), the
[command reference](docs/cli.md), or the normative [`spec/_.hint`](spec/_.hint) with `hint spec`.

## What you can do

| Capability | Commands and result |
| --- | --- |
| Dependency wiring | `add`, `wire`, `update`, `remove` edit native npm/Python/Go/Rust/Swift/Dart/Ruby/PHP/Elixir/Haskell/Zig/Clojure/Nix files at one resolved commit. |
| Source changes | `set`, `pin`, and `unpin` transactionally change URL, ref, monorepo path, tracking, or identity. |
| Owners and contacts | `who` routes an intent and path to the declared role, scoped agent, and ordered contacts. |
| Agent roster | `sync` maintains a bounded dependency/owner block in `AGENTS.md` or another target. |
| Contact delivery | `contact` sends through A2A or GitHub Issues; URL, email, and chat kinds print exact instructions. |
| Cards, catalog, and trust | `card export/validate/verify/show` and `catalog export` project A2A v1.0 cards, ARD catalogs, and JWS trust. |
| Liveness and drift | `status` compares upstream refs, manifest/cache hashes, native wiring, cards, trust, and synced context. |
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

## Installation

Go-native channels are the simplest and come first. Pin `@v1.0.0` rather than `@latest` in CI:

```sh
go install github.com/neprel/git-a2a/cmd/git-a2a@v1.0.0
go run github.com/neprel/git-a2a/cmd/git-a2a@v1.0.0 --version
```

The checksum-verifying standalone installers support `GIT_A2A_VERSION`, `--version`, `--dir`,
and `--dry-run`:

```sh
curl -fsSL https://git-a2a.com/install.sh | bash
```

```powershell
irm https://git-a2a.com/install.ps1 | iex
```

Package-manager and zero-install channels:

```sh
brew install neprel/tap/git-a2a
scoop bucket add git-a2a https://github.com/neprel/scoop-bucket
scoop install git-a2a
npx git-a2a --version
uvx git-a2a --version
```

Linux `.deb`, `.rpm`, and `.apk` packages are attached to every GitHub Release:

```sh
sudo dpkg -i git-a2a_*.deb
sudo rpm -i git-a2a_*.rpm
sudo apk add --allow-untrusted git-a2a_*.apk
```

The scratch container contains only the binary:

```sh
docker run --rm ghcr.io/neprel/git-a2a:1.0.0 version
```

Every channel executes the same Go binary. `git-a2a version --check` is the only automatic
release lookup and prints the correct manager-specific update command. `git-a2a upgrade` is
available only to the standalone binary channel; it never overwrites a package-manager install.
Release archives cover Darwin, Linux, and Windows on amd64/arm64 and include checksums and SBOMs.
Maintainer setup is in [docs/releasing.md](docs/releasing.md).

## A2A, AGENTS.md, and Agent Skills

- **A2A** is the agent-to-agent protocol. Agent Cards remain native A2A v1.0; the
  `https://git-a2a.com/ext/module/v1` extension binds a card to a Git module without copying its
  self-description. `contact` can deliver A2A `SendMessage` requests.
- **AGENTS.md** is a consumer-facing context surface. `sync` renders only published module,
  surface, ownership, and routing data into a managed block; it never imports dependency
  instructions or private implementation knowledge.
- **Agent Skills** teach a harness how to perform a task. They complement git-a2a: a Skill may
  call this CLI, while `a2amodule.yml` remains the durable, harness-neutral ownership and
  dependency contract shared by every agent environment.

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
