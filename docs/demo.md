# Public polyglot demo

The public demo is a small owner/consumer pair with no private infrastructure:

- [`neprel/git-a2a-demo-acme-lib`](https://github.com/neprel/git-a2a-demo-acme-lib) publishes
  equivalent TypeScript, Python, Go, and header-only C++ utilities. Its manifest declares exports,
  public surface, agents, working GitHub Issue routes, and static A2A cards.
- [`neprel/git-a2a-demo-acme-app`](https://github.com/neprel/git-a2a-demo-acme-app) consumes one
  resolved library commit through npm, uv, Go, and CMake. The C++ source is a Git submodule at
  `deps/acme-lib-utils`; every language builds in CI from the same lock.

## What to inspect

1. In the library, read `a2amodule.yml`, then only the owner-published `surface/`. Inspect the
   four exports and the `acme-lib-utils` / `acme-pm` ownership routes.
2. Inspect the static cards and ARD catalog served from
   `https://git-a2a.com/demo/agents/`. They are generated from `card export` / `catalog export`,
   not hand-authored second descriptions.
3. In the consumer, compare `a2amodule.yml` with `a2amodule.lock`: the dependency commit and
   `vendor: {mode: submodule}` agree with `.gitmodules` and the gitlink.
4. Compare `package.json`, `pyproject.toml`, `go.mod`, and the generated CMake include. All point
   through the vendored path; the status table therefore includes `VENDOR`.
5. Read only the managed portion of `AGENTS.md`, then inspect `.github/workflows/ci.yml` for the
   reproducible fresh-clone sequence.

## Run it

```sh
git clone --recurse-submodules https://github.com/neprel/git-a2a-demo-acme-app.git consumer-app
cd consumer-app
git-a2a fetch
git-a2a status --offline
git-a2a who acme-lib-utils --intent change
git-a2a show acme-lib-utils --surface
cmake -S . -B build
cmake --build build
```

A clean status includes one dependency row with `VENDOR submodule @<commit>`, followed by
`consumer-app: manifest valid` and `1 dependency: clean`.

Use `git-a2a update --check` as an informational CI step: a moving demo branch may advance, so
exit `1` reports available work rather than authorising an automatic rewrite. For a deliberate
lifecycle exercise, follow the repository README through `set --ref v1.1.0`, `pin`, `unpin`,
`card export`, `catalog export`, and `contact --intent change`.

The demo trust setup uses public, demo-only signing material. The consumer's allowed-signers
file and pinned card JWKS demonstrate verification and include a negative throwaway unsigned-fork
check; they are examples, never production credentials. See [Trust](trust.md) and
[Vendored dependencies](vendoring.md).
