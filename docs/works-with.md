# Supported adapters

Adapters own the platform-specific add, pull, remove, and local-inspection lifecycle. Initial
selection is persisted per dependency, including the concrete package-manager variant. All
bindings of a dependency use one resolved commit.

| Mode | Platform or build system | Typical variants / owned files |
| --- | --- | --- |
| Native | npm-family | npm, Yarn, pnpm, Bun; `package.json` and manager lockfile |
| Native | Python | uv, Poetry, PDM, PEP 621/pip; project declarations, manager lockfiles where applicable, and target environments |
| Native | Go | Go modules (`go.mod`, `go.sum`) |
| Native | Cargo | Cargo manifest and lockfile |
| Native | Swift | Swift Package Manager |
| Native | Pub | Dart/Flutter Pub |
| Native | Gem | Bundler/RubyGems |
| Native | Composer | PHP Composer |
| Native | Hex | Elixir Mix/Hex |
| Native | Hackage | Cabal and Stack |
| Native | Zig | Zig package metadata |
| Native | Clojure | Clojure dependency declarations |
| Native | Nix | Nix flake/dependency declarations |
| Submodule + build | CMake | Generated/managed CMake integration referencing the shared checkout |
| Submodule + build | Gradle | Managed Gradle integration referencing the shared checkout |
| Submodule + build | MSBuild | Managed project/import integration referencing the shared checkout |
| Submodule + build | Maven | Managed Maven integration referencing the shared checkout |
| Submodule + build | Meson | Managed Meson integration referencing the shared checkout |
| Submodule only | Git | Fallback when no integration applies or the native source is unsupported |

The build adapters do not clone or copy a second source tree. They compose with the dependency's
single submodule materialization. MSBuild support describes the existing source/project
integration; it is not a promise that arbitrary Git repositories can be installed as NuGet
packages.

A tool missing during `pull` is an actionable failure, not permission to switch variants.
Adapters preserve unrelated native entries and user changes, and native lockfiles remain under
their package manager's control.

## Lifecycle evidence

Every adapter has editor, capability, repeated-Pull, Inspect, Remove, and failure-before-mutation
tests. Every registered variant also has a tracked public-CLI native fixture wired into the
mandatory container matrix. A fixture or CI declaration is not itself execution evidence: only a
runner summary row with `PASS`, an observed manager version, and its retained log verifies that
variant. `FAIL`, `BLOCKED`, and `NOT RUN` must remain visible and must not be promoted to a support
claim.

### Reproducible native matrix

The tracked runner enumerates every family and concrete variant in the current registry. It uses
fixed-version Linux images, builds the Linux CLI and Go test entry points inside a pinned Go image,
and exercises a snapshot of the current dirty tree:

```sh
python3 tools/native-lifecycle/run.py --group all --keep-going
```

`--group python` runs uv, Poetry, PDM, and PEP 621/pip. Individual rows can be selected with
`--case ID`; `--list` prints all row IDs. Every row is a mandatory CI matrix job and runs a
public-CLI fixture against the real manager; editor mocks and direct adapter calls are not accepted
as native lifecycle evidence.

Each run writes one log per row and both JSON and Markdown summaries below
`build/native-lifecycle/`. The summary records the family, saved variant, pinned image,
Linux/amd64 target, observed tool version, exact test command, evidence kind, and
`PASS`/`FAIL`/`BLOCKED`/`NOT RUN` status. A missing mandatory manager or skipped test is a failure.
An image or Docker-runtime failure is reported as blocked, never as passed. The runner mounts source and
binaries read-only and does not forward the Docker socket, SSH agent, host Git configuration, or
credentials; fixtures and remotes are local to the temporary container.

Linux container results do not prove macOS- or Windows-specific behavior. Cross-compilation is
reported separately from native execution, and command mocks remain form-level evidence only.
