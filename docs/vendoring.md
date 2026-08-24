# Vendored dependencies

Vendoring places a dependency's locked source inside the consumer while keeping
`a2amodule.lock` authoritative. Use it when a build system needs a local project, a checkout must
remain buildable without package-host access, or the consumer deliberately reviews source as part
of its repository. Prefer native Git dependencies when none of those conditions apply.

## Submodule or copy

| | Submodule | Copy |
|---|---|---|
| Clone ergonomics | Requires `--recurse-submodules` or `git-a2a fetch` | Ordinary clone contains files |
| Repository size | Source objects remain in the linked repository | Source bytes become consumer history |
| Offline checkout | Works after submodule objects are present | Works immediately after clone |
| Git history | Preserves explicit source repository and gitlink | Records only consumer-side file changes |
| Drift model | Gitlink, URL, worktree HEAD, dirty files | Locked Git tree hash versus copied tree |

Copy mode rejects nested submodules because silently flattening their identity would make the
lock incomplete. Neither mode follows a moving ref during repair.

## Lifecycle

```sh
git-a2a add https://github.com/acme/lib-utils.git --vendor submodule
git-a2a set acme-lib-utils --vendor copy
git-a2a update acme-lib-utils
git-a2a fetch acme-lib-utils
git-a2a set acme-lib-utils --no-vendor
git-a2a remove acme-lib-utils
```

The default path is `deps/<id>`; use `--vendor-path PATH` when a build system requires another
location (Meson conventionally uses `subprojects/<id>`). Dirty or drifted materialisation blocks
`set`, `update`, and `remove`; `--force` is the explicit data-loss approval. Each mutation stages
the source, adapter files, manifest, lock, and cache as one rollback-capable operation.

## Build systems

| Build system | Generated git-a2a file | One managed root entry |
|---|---|---|
| CMake | `deps/git-a2a.cmake` | `include(deps/git-a2a.cmake)` |
| Gradle | `deps/git-a2a.settings.gradle(.kts)` | one `apply from` in settings |
| Maven | `deps/git-a2a-pom.xml` | one managed module entry in `pom.xml` |
| MSBuild | `deps/git-a2a.targets` | one project import |
| Meson | `deps/git-a2a/meson.build` | one `subdir()` entry |

Generated integration files belong entirely to git-a2a. `wire` replaces unexpected content with
a warning, while `remove`/unwire can always delete it. Maven uses a generated reactor module;
Maven has no composite-build equivalent.

## Native path mode

When a dependency is vendored, npm uses `file:`, Cargo uses `path`, Go adds a local `replace`,
Pub and Mix use path dependencies, uv uses a path source, and Composer uses a `type: path`
repository. Removing vendoring returns each entry to its exact locked Git form. Package-manager
Refresh still requires the native tool reported by `doctor`; deterministic wiring itself does not.

## CI

```sh
git clone --recurse-submodules https://github.com/acme/consumer-app.git
cd consumer-app
git-a2a fetch
git-a2a status --offline
cmake -S . -B build
cmake --build build
```

On Windows, lock hashes come from Git blobs so `core.autocrlf` cannot change them. Submodule
junctions and symlinks are inspected through their resolved paths; copy mode does not create
symlinks. Commit `.gitmodules` and the gitlink for submodule mode, or the copied tree for copy
mode, together with the manifest, lock, and adapter changes.

See the [consumer guide](consuming.md), [demo](demo.md), and [CLI reference](cli.md).
