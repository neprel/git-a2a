# git-a2a

**A git repository that can be imported together with the agents that own it.**

git-a2a is a small standard plus a CLI for a *micro-agent architecture*: every repository is a
module owned by one or more AI agents, and dependencies are taken on agents at developer time —
the way microservices take dependencies on services at runtime.

- **`a2amodule.yml`** at the repository root says what the module is, how to import it in each
  ecosystem (npm, PyPI, Go, …), which agents own which part of it, how each agent wants to be
  contacted for each kind of request (a question, a change request, a bug — over A2A, a chat
  channel, an issue tracker, email), what consumers may and may not do, and which other modules
  it depends on. `a2amodule.lock` records what was resolved.
- **`git-a2a`** (also `git a2a …`) adds a dependency from a git URL by fetching *only its
  manifest*, wires it into every package manager the consuming repository uses at one resolved
  commit, keeps the lock, renders a short roster of dependencies and their owners into
  `AGENTS.md`, answers "who do I ask about this and how", exports and validates A2A agent
  cards, and reports liveness and drift.

Status: **specification draft, CLI not yet released.** The normative spec is
[`spec/_.hint`](spec/_.hint) (read it with `hint spec`); worked examples are in
[`spec/examples/`](spec/examples/). Agent cards follow the
[A2A protocol](https://a2a-protocol.org/) v1.0; git-a2a adds semantics through the extension
`https://git-a2a.com/ext/module/v1`.

```sh
git-a2a init                                   # describe this repository
git-a2a add ssh://git@github.com/acme/lib-utils.git   # depend on a module: manifest + wiring + lock
git-a2a who acme-lib-utils --intent change     # who owns it, how they want to be asked
git-a2a status                                 # upstream, wiring, agents: up / behind / drifted
```

License: MIT.

## Installation

Tagged releases are built and published entirely by GitHub Actions. Every channel installs
the same static Go binary:

```sh
brew install neprel/tap/git-a2a
npx git-a2a --version
uvx git-a2a --version
go install github.com/neprel/git-a2a/cmd/git-a2a@latest
curl -fsSL https://raw.githubusercontent.com/neprel/git-a2a/main/install.sh | sh
```

For an agent container, copy a release binary into the image; no runtime is required:

```dockerfile
COPY git-a2a /usr/local/bin/git-a2a
RUN chmod 0755 /usr/local/bin/git-a2a
```

GitHub Releases contain archives for Darwin, Linux, and Windows on amd64 and arm64, checksums,
and SBOMs. The npm and PyPI packages are thin launchers around those exact binaries.
