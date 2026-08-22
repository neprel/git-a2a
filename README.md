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

Status: **early release.** The normative spec is
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
go install github.com/neprel/git-a2a/cmd/git-a2a@latest
go run github.com/neprel/git-a2a/cmd/git-a2a@latest --version
curl -fsSL https://raw.githubusercontent.com/neprel/git-a2a/main/install.sh | sh
brew install neprel/tap/git-a2a
scoop bucket add git-a2a https://github.com/neprel/scoop-bucket
scoop install git-a2a
npx git-a2a --version
uvx git-a2a --version
```

Pin a tag instead of `@latest` in CI and image builds. `git-a2a version --check` performs an
explicit release check and prints the correct update command for the install channel; plain
`version` never uses the network. `git-a2a upgrade` replaces the executable only for the
standalone `binary` channel. Package-manager installations must be upgraded by that manager.

Linux packages are attached to each GitHub Release. After downloading the matching asset, use
`sudo dpkg -i git-a2a_*.deb`, `sudo rpm -i git-a2a_*.rpm`, or `sudo apk add --allow-untrusted
git-a2a_*.apk`. The scratch container can be run directly with `docker run --rm
ghcr.io/neprel/git-a2a:0.1.0 version`.

For an agent container, add the matching release archive; Docker extracts its `git-a2a` binary
and no runtime is required:

```dockerfile
ARG GIT_A2A_VERSION=1.0.0
ARG TARGETARCH
ADD git-a2a_${GIT_A2A_VERSION}_linux_${TARGETARCH}.tar.gz /usr/local/bin/
RUN chmod 0755 /usr/local/bin/git-a2a
```

GitHub Releases contain archives for Darwin, Linux, and Windows on amd64 and arm64, checksums,
SBOMs, deb/rpm/apk packages, and a `ghcr.io/neprel/git-a2a` scratch image. The npm and PyPI
packages are convenience launchers and do not block a release.

Maintainer setup, optional secrets, prerelease behavior, and signing limitations are documented
in [`docs/releasing.md`](docs/releasing.md).
