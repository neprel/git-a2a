# git-a2a

[![built with HINT](https://img.shields.io/badge/built_with-HINT-5b4ee6)](https://openhint.dev/)
[![release](https://img.shields.io/github/v/release/neprel/git-a2a)](https://github.com/neprel/git-a2a/releases)
[![CI](https://github.com/neprel/git-a2a/actions/workflows/ci.yml/badge.svg)](https://github.com/neprel/git-a2a/actions/workflows/ci.yml)
[![npm](https://img.shields.io/npm/v/git-a2a)](https://www.npmjs.com/package/git-a2a)
[![PyPI](https://img.shields.io/pypi/v/git-a2a)](https://pypi.org/project/git-a2a/)
[![Homebrew](https://img.shields.io/badge/homebrew-neprel%2Ftap-fbb040)](https://github.com/neprel/homebrew-tap)

**Connect a Git component together with the agent responsible for it.**

git-a2a is a cross-platform component dependency manager. A component repository publishes
code, one A2A Agent Card reference for its owner, and optionally a readable surface such as API
documentation, examples, or source. A consumer connects the code through its own platform's
adapter and records the exact commit, adapter, and package-manager variant.

git-a2a does not send messages, manage tasks, run agents, or host Agent Cards. Use the usable Agent Card reference
shown by `git a2a list` with an external A2A client: an HTTPS URL or a local copy of a
repository-relative card from the applied commit.

## Quick start

Run the binary directly as `git-a2a`, or install it on `PATH` and use Git's equivalent
`git a2a` form:

```sh
git a2a init
git a2a add https://github.com/acme/lib-utils.git --name lib-utils
git a2a list lib-utils
git a2a pull lib-utils
git a2a remove lib-utils
```

There are exactly five domain commands:

| Command | Result |
| --- | --- |
| `init` | Creates a minimal schema 2 `a2amodule.yml` without overwriting an existing manifest. |
| `add SOURCE` | Resolves one commit, chooses applicable adapters, connects the component, and records its owner and surface. |
| `pull [NAME]` | Updates one or every dependency through the adapters and variants chosen by `add`; missing materialization is restored. |
| `remove NAME` | Removes only the selected dependency's owned integration and local materialization. |
| `list [NAME]` | Reports local dependency, commit, adapter, Agent Card, surface, and problem information; `--json` is available. |

`pull` is adapter lifecycle, not a wrapper around `git pull`. All bindings of a polyglot
component use the same resolved commit. Adapter and manager selection is durable: a later change
to `PATH` or consumer marker files cannot silently switch it.

## End-to-end demo

The [ACME app walkthrough](https://github.com/neprel/git-a2a-demo-acme-app#readme) demonstrates
the complete loop: connect a component, discover its responsible agent, send a request through
A2A, let the owner change the library, run `git-a2a pull`, and observe the new result. The
[app repository](https://github.com/neprel/git-a2a-demo-acme-app) and
[library repository](https://github.com/neprel/git-a2a-demo-acme-lib) must be cloned next to each
other. Docker with Compose is the only runtime requirement:

```sh
git clone https://github.com/neprel/git-a2a-demo-acme-lib.git
git clone https://github.com/neprel/git-a2a-demo-acme-app.git
cd git-a2a-demo-acme-app
./demo/run.sh

# Short npm-only profile
DEMO_PROFILE=npm ./demo/run.sh
```

The demo uses deterministic agents without LLM API keys, real A2A transport, the published
git-a2a 2.0.0, and temporary local Git remotes. The separate demo client performs A2A
communication; git-a2a manages dependency code and metadata. The agents exist only for the local
run and are not permanently available public services. See the
[verified transcript](https://github.com/neprel/git-a2a-demo-acme-app/blob/main/docs/demo-transcript.md)
for the recorded full-profile result.

## Component contract

`a2amodule.yml` uses schema 2:

```yaml
schema: 2
component:
  id: lib-utils
  description: Shared parsing and validation utilities.
  repository: https://github.com/acme/lib-utils.git
  exports:
    - adapter: npm
      name: "@acme/lib-utils"
    - adapter: pypi
      name: acme-lib-utils
  surface: docs/public
agent:
  name: lib-utils-owner
  card: https://agents.acme.example/lib-utils/.well-known/agent-card.json
```

The Agent Card remains the authority for the agent's interfaces, skills, and authentication.
The manifest deliberately does not duplicate them. `init` may create an incomplete local
manifest without `agent`; a repository consumed by `add` or `pull` must declare `agent.card`.

The optional `component.surface` is published data from the same applied commit and is
materialized under `.git-a2a/surfaces/NAME`. If no surface is declared, no repository content is
implicitly published. Surface is not an installation mechanism and is not a submodule.

See the [manifest reference](docs/manifest-reference.md), [authoring guide](docs/authoring.md),
[consumer guide](docs/consuming.md), and [schema 1 migration guide](docs/migration-v2.md).

## Adapter matrix

Every existing platform integration is retained. Native adapters apply dependencies through the platform's own
manager, updating declarations, native locks, and installed or resolved state; build-system adapters compose with one submodule checkout rather than copying
source into a second tree.

| Mode | Adapters |
| --- | --- |
| Native Git dependency | npm (npm, Yarn, pnpm, Bun), Python (uv, Poetry, PDM, PEP 621/pip), Go, Cargo, SwiftPM, Pub, Bundler, Composer, Mix, Cabal/Stack, Zig, Clojure, Nix |
| Submodule + build integration | CMake, Gradle, MSBuild, Maven, Meson |
| Submodule only | Fallback when no integration applies or the native manager cannot represent the source |

Git submodule is an ordinary adapter with the same add/pull/remove/inspect lifecycle. Native
lockfiles remain owned by their package managers. See [Works with](docs/works-with.md) for the
detailed matrix and boundaries.

## Installation

These existing installation channels are preserved. Commands track the latest stable release;
pin a version in CI.

<!-- generated-facts:channels:start -->
| Channel | Command |
| --- | --- |
| Go | `go install github.com/neprel/git-a2a/v2/cmd/git-a2a@latest` |
| Go zero-install | `go run github.com/neprel/git-a2a/v2/cmd/git-a2a@latest --version` |
| macOS/Linux installer | `curl -fsSL https://git-a2a.com/install.sh \| bash` |
| Windows installer | `irm https://git-a2a.com/install.ps1 \| iex` |
| Homebrew | `brew install neprel/tap/git-a2a` |
| Scoop | `scoop bucket add git-a2a https://github.com/neprel/scoop-bucket; scoop install git-a2a` |
| npm | `npx git-a2a@latest --version` |
| PyPI with uv | `uvx git-a2a --version` |
| PyPI with pipx | `pipx run git-a2a --version` |
| Container | `docker run --pull=always --rm ghcr.io/neprel/git-a2a:latest --version` |
| Nix flake | `nix run github:neprel/git-a2a -- --version` |
<!-- generated-facts:channels:end -->

Linux `.deb`, `.rpm`, and `.apk` packages are attached to GitHub Releases. Release archives
cover Darwin, Linux, and Windows on amd64/arm64 and include checksums and SBOMs. The standalone
installers verify checksums and support version pinning, destination selection, and dry-run.
Package-manager installs are updated with that package manager; the binary has no self-updater.

Maintainer and provenance details are in [the release guide](docs/releasing.md).

## Documentation

- [CLI reference](docs/cli.md)
- [Manifest reference](docs/manifest-reference.md)
- [Authoring a component](docs/authoring.md)
- [Consuming components](docs/consuming.md)
- [Agents and external A2A clients](docs/agents.md)
- [Supported adapters](docs/works-with.md)
- [Migrating from schema 1](docs/migration-v2.md)
- [FAQ](docs/faq.md)
- [Release and installation maintenance](docs/releasing.md)

The portable [Agent Skill](skills/git-a2a/SKILL.md) teaches a coding agent this five-command
workflow. It does not configure an MCP server or agent harness.

## Specification as source

Repository decisions and invariants live in `.hint` files beside the artifacts they govern.
`hint <path>` returns the knowledge governing a path, and `hint check <path...>` validates the
linked research records. The repository and CI use HINT 2.0.1. See
[HINT](https://openhint.dev/) for the tool and format.

License: MIT.
