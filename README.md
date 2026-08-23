# git-a2a

[![built with HINT](https://img.shields.io/badge/built_with-HINT-5b4ee6)](https://openhint.dev/)

**Import a Git repository together with the agents that own it.**

git-a2a is an open standard plus a static Go CLI for micro-agent architectures. A repository
publishes one `a2amodule.yml`: what the module exports, which agents own it, how to contact them,
and which other modules it consumes. The CLI resolves one Git commit, wires every detected
ecosystem, maintains `a2amodule.lock`, and projects the useful ownership context into agent tools.

Status: **1.0.1 — stable.** Read the [human specification](spec/README.md), the
[command reference](docs/cli.md), or the normative [`spec/_.hint`](spec/_.hint) with `hint spec`.

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

Go-native channels are the simplest and come first. Every checked channel below was exercised
against the public 1.0.1 artifact on its native platform. Pin versions in CI.

| Verified | Channel | Command |
| --- | --- | --- |
| ✔ | Go | `go install github.com/neprel/git-a2a/cmd/git-a2a@v1.0.1` |
| ✔ | Go zero-install | `go run github.com/neprel/git-a2a/cmd/git-a2a@v1.0.1 --version` |
| ✔ | macOS/Linux installer | `curl -fsSL https://git-a2a.com/install.sh \| bash` |
| ✔ | Windows installer | `irm https://git-a2a.com/install.ps1 \| iex` |
| ✔ | Homebrew | `brew install neprel/tap/git-a2a` |
| ✔ | Scoop | `scoop bucket add git-a2a https://github.com/neprel/scoop-bucket; scoop install git-a2a` |
| ✔ | npm | `npx git-a2a@1.0.1 --version` |
| ✔ | PyPI with uv | `uvx git-a2a@1.0.1 --version` |
| ✔ | PyPI with pipx | `pipx run git-a2a==1.0.1 --version` |
| ✔ | Container | `docker run --rm ghcr.io/neprel/git-a2a:1.0.1 --version` |

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
docker run --rm ghcr.io/neprel/git-a2a:1.0.1 version
```

Every channel executes the same Go binary. `git-a2a version --check` is the only automatic
release lookup and prints the correct manager-specific update command. `git-a2a upgrade` is
available only to the standalone binary channel; it never overwrites a package-manager install.
Release archives cover Darwin, Linux, and Windows on amd64/arm64 and include checksums and SBOMs.
Maintainer setup is in [docs/releasing.md](docs/releasing.md).

## Documentation

- [Manifest field reference](docs/manifest-reference.md): generated types, defaults, values, and consequences for every field.
- [Authoring guide](docs/authoring.md): create and publish a module, its surface, agents, contacts, policy, and cards.
- [Consumer demo](docs/demo.md): inspect the public polyglot library and app end to end.
- [CLI reference](docs/cli.md): exact commands, flags, outputs, and exit codes.

## A2A, AGENTS.md, and Agent Skills

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
  for detected Claude Code, Codex, Cursor, Copilot, Gemini CLI, and OpenCode environments.

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
