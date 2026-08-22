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
  `neprel/homebrew-tap`. If absent, GoReleaser skips Homebrew publishing.
- `SCOOP_BUCKET_TOKEN`: a fine-grained token with write access to
  `neprel/scoop-bucket`. If absent, GoReleaser skips Scoop publishing.
- `NPM_TOKEN`: npm automation token permitted to publish `git-a2a` and the platform packages.
  If absent, the npm publish step reports a clean skip. npm 11.5+ trusted publishing can replace
  this secret once the GitHub workflow is registered on npm; remove the secret gate only then.
- PyPI: create the GitHub environment named `pypi`, register this repository and workflow as a
  trusted publisher for the `git-a2a` project, and keep its approval rules in that environment.
  PyPI uses OIDC and has no long-lived token.

Job permissions are intentionally local: tests have read-only contents; GoReleaser alone has
`contents: write` and `packages: write`; npm and PyPI alone have `id-token: write`.

## macOS and Windows status

The Homebrew cask artifacts are currently not Apple-signed or notarized. This is an explicit
early-release limitation: users may need to remove the downloaded-file quarantine attribute,
and signing/notarization should be configured before claiming Gatekeeper-clean installation.
The Scoop bucket is automated. winget submission is deliberately deferred until the stable
package identity and publisher account exist; it is not part of the first automated release.

## Release checklist

1. Set `internal/version/VERSION`, commit it, and run `.github/scripts/check-version.sh v<VERSION>`
   and `.github/scripts/check-version.sh v<VERSION>-rc.1`. npm keeps the SemVer prerelease form;
   PyPI maps `-rc.N` to its equivalent `rcN` spelling.
2. Run `go test ./...`, `go build ./...`, `goreleaser check`, and `goreleaser release --snapshot --clean`.
3. Push the tag. Verify GitHub archives, checksums, SBOMs, deb/rpm/apk, GHCR, and every configured
   optional channel. A prerelease tag must remain a GitHub prerelease and must not become latest.
4. Create the stable tag on the exact reviewed commit only after the release-candidate path is
   clean. If any source changed, run a new release candidate first.
