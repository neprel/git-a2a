# Releasing git-a2a

Releases are created only by `.github/workflows/release.yml` after a `v*` tag is pushed. The tag
must equal `v` plus `internal/version/VERSION`, optionally followed by a valid SemVer prerelease
suffix; build and tests must pass before publishing starts.
Use a release-candidate tag first, inspect every generated artifact and channel, fix the source,
then create the stable tag on the reviewed commit. If the candidate needs a fix, use a new
prerelease number on the fixing commit. Do not rerun a failed publish with a moved tag.

## GitHub configuration

The workflow uses the repository `GITHUB_TOKEN` for GitHub Releases and
`ghcr.io/neprel/git-a2a`. Configure these optional channels separately:

- `HOMEBREW_TAP_TOKEN`: a fine-grained token with write access to
  `neprel/homebrew-tap`. If absent, the channel job skips Homebrew publishing.
- `SCOOP_BUCKET_TOKEN`: a fine-grained token with write access to
  `neprel/scoop-bucket`. If absent, the channel job skips Scoop publishing.
- npm: `git-a2a` and every `@git-a2a/*` platform package register `neprel/git-a2a` and
  `release.yml` as their GitHub Actions trusted publisher with `npm publish` permission. The
  job uses Node 24, npm 11.5.1 and OIDC; it has no long-lived npm token. Prereleases receive
  npm dist-tag `next`; stable releases receive `latest`.
- PyPI: create the GitHub environment named `pypi`, register this repository and workflow as a
  trusted publisher for the `git-a2a` project, and keep its approval rules in that environment.
  PyPI uses OIDC and has no long-lived token.

Job permissions are intentionally local: tests have read-only contents; GoReleaser alone has
`contents: write` and `packages: write`; npm and PyPI alone have `id-token: write`. Each npm
package includes repository metadata matching `https://github.com/neprel/git-a2a`, which npm
requires when authenticating its trusted publisher.

A `workflow_dispatch` recovery checks out the immutable tag, copies that tag's GoReleaser config
outside the checkout, and injects `release.skip_upload: true` into the copy before rebuilding.
The checkout remains clean and existing GitHub release assets remain untouched. The channel job
downloads the published `checksums.txt` and renders Homebrew/Scoop manifests from those immutable
asset hashes; it must never use hashes from a recovery rebuild, whose archives need not be
byte-identical. npm checks each immutable package version and leaves an already-published version
unchanged. Tag-triggered releases use the checked-in config with artifact upload enabled.

## macOS and Windows status

The macOS binaries are not yet Apple-signed or notarized. A cask or direct browser download is
therefore quarantined and Gatekeeper rejects its first launch. The tap publishes a checksum-
verified formula instead: after Homebrew verifies the immutable release SHA-256, its install step
removes `com.apple.quarantine` from that one binary. `brew test neprel/tap/git-a2a` must exercise
the first launch. Apple signing/notarization remains required before publishing a cask or claiming
that direct downloads are Gatekeeper-clean.
The Scoop bucket is automated. winget submission is deliberately deferred until the stable
package identity and publisher account exist; it is not part of the first automated release.
Run `gh workflow run installers.yml -f live_channels=true` after publishing a stable manifest;
the `scoop-live` job installs from the public bucket on `windows-latest` and asserts the exact
version, target, and `channel=scoop`. Wine under amd64 QEMU on Apple Silicon is not an equivalent
gate: the container runtime can abort before the Windows executable starts.

The manually published site includes an Apache `.htaccess` that serves `.sh` and `.ps1` as
UTF-8 `text/plain`. After every change to that file or either installer, publish with
`make site-publish`, then run `gh workflow run installers.yml -f live_site=true`. The
`site-live` job checks the live `Content-Type`, downloads `install.ps1` with
`Invoke-RestMethod`, compiles the returned text, and runs the installer with `--dry-run` on
`windows-latest`. Do not infer this result from a local server: only the production host proves
that its `.htaccess` is enabled.

The v1.0.0 acceptance run is [Installer checks #32626028405](https://github.com/neprel/git-a2a/actions/runs/32626028405).
Its live Windows output was:

```text
text/plain; charset=utf-8
dry-run: resolve v1.0.0 from https://github.com/neprel/git-a2a/releases
dry-run: download and SHA-256 verify git-a2a_1.0.0_windows_amd64.zip
dry-run: install git-a2a.exe to the runner temporary directory
```

The first complete stable tag-triggered publication is
[Release #32629413793](https://github.com/neprel/git-a2a/actions/runs/32629413793) for v1.0.1:
test, GoReleaser/GHCR, npm OIDC, PyPI OIDC, Homebrew, and Scoop all completed successfully.
[Installer checks #32629684742](https://github.com/neprel/git-a2a/actions/runs/32629684742)
then installed Scoop 1.0.1 on `windows-latest`; the version assertion is derived from
`internal/version/VERSION`. A native Homebrew reinstall also proved that conditional quarantine
removal succeeds when `com.apple.quarantine` is already absent.

Before a stable release, manually exercise replacement of an installed binary on Windows:

```powershell
$env:GIT_A2A_UPGRADE_BASE_URL = "https://github.com/neprel/git-a2a/releases/download"
git-a2a upgrade --to 1.0.0-rc.1
git-a2a --version
git-a2a upgrade --to 1.0.0
git-a2a --version
```

Run this from a fresh PowerShell process with the binary installed outside the checkout. Confirm
that the old process exits, the `.new` file is renamed into place, and a second invocation leaves
no `.old` or `.new` sibling. The ordinary CI workflow runs `go test ./...` on `windows-latest`;
this manual check covers the live executable-replacement path that a unit test cannot own.

## Release checklist

1. Set `internal/version/VERSION`, commit it, and run `.github/scripts/check-version.sh v<VERSION>`
   and `.github/scripts/check-version.sh v<VERSION>-rc.1`. npm keeps the SemVer prerelease form;
   PyPI maps `-rc.N` to its equivalent `rcN` spelling.
2. Run `go test ./...`, `go build ./...`, `scripts/a2a-schema.sh`, `goreleaser check`, and
   `goreleaser release --snapshot --clean`. The conformance script fetches the pinned official
   A2A proto and pinned Google API imports into a temporary directory, generates the non-normative
   JSON Schema with a pinned generator, and validates both card and catalog exports.
3. Push the tag. Verify GitHub archives, checksums, SBOMs, deb/rpm/apk, GHCR, and every configured
   optional channel. A prerelease tag must remain a GitHub prerelease and must not become latest.
4. Run `gh workflow run release-smoke.yml -f tag=v<VERSION>` and require all three native jobs
   (Linux, macOS, Windows) to print the canonical source version (without an RC package suffix),
   list the eight default MCP tools through stdio, and complete `setup --dry-run` against the
   downloaded release binary.
5. Create the stable tag on the exact reviewed commit only after the release-candidate path is
   clean. If any source changed, run a new release candidate first.
