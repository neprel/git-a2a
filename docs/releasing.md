# Release and installation maintenance

This guide preserves git-a2a's binary distribution channels. It does not authorize publishing a
release, creating a stable tag, or deploying the website.

## Supported channels

One Go binary is distributed through:

- `go install` and `go run`
- checksum-verifying macOS/Linux and Windows installers
- Homebrew and Scoop
- npm launcher package
- PyPI launchers for uv and pipx
- GHCR scratch container
- Nix flake
- GitHub Release archives and Linux `.deb`, `.rpm`, and `.apk` packages

Release archives cover Darwin, Linux, and Windows on amd64/arm64 and include checksums and SBOMs.
The package-manager launchers execute the same release binary. git-a2a has no self-updater;
users update it through the channel that installed it.

## Recovery safety

Run recovery from the current `main` workflow and pass the existing immutable tag. If its GitHub
Release already exists, the workflow never rebuilds or uploads those assets and never overwrites an
existing versioned GHCR image or npm package. A missing GHCR version is assembled only from
checksum-verified release archives. PyPI recovery compares all six expected wheel filenames and
checksums, uploads only missing wheels, and fails on registry errors or checksum conflicts.

All tag-triggered releases and manually dispatched recoveries share one repository-wide publication
lock and do not cancel the active run. The release policy is evaluated only after that lock is
acquired, so a queued older recovery observes any newer release completed ahead of it.

Mutable stable channels are promoted only when the target is at least the newest published stable
release. Recovering an older tag therefore leaves GHCR `latest`, Homebrew, Scoop, and npm `latest`
unchanged. A prerelease never changes those stable channels; npm `next` moves only to an equal or
newer prerelease. New npm packages are published directly under the eligible `latest` or `next` tag using
GitHub OIDC. Older-version recovery uses a permanent `release-VERSION` tag and does not modify
stable or prerelease tags. OIDC does not authorize separate dist-tag mutations: an existing
version with an incorrect tag requires an authenticated maintainer to retag it, or a new release.
The npm job fails visibly if an eligible channel does not point to the expected version.

## Verification

Tag-triggered release assets carry GitHub build provenance. After downloading an asset:

```sh
gh attestation verify PATH/TO/ASSET \
  --repo neprel/git-a2a \
  --signer-workflow neprel/git-a2a/.github/workflows/release.yml
sha256sum --ignore-missing -c checksums.txt
```

The GHCR image is signed keylessly at its immutable digest. Verify the GitHub OIDC issuer and the
release workflow identity. Normal releases use the immutable tag ref; a recovery that completes an
already-created immutable tag uses the protected `main` workflow ref:

```sh
cosign verify \
  --certificate-identity-regexp '^https://github\.com/neprel/git-a2a/\.github/workflows/release\.yml@(refs/tags/v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?|refs/heads/main)$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/neprel/git-a2a@sha256:DIGEST
```

The standalone installers must verify checksums and retain explicit version, destination, and
dry-run controls. They must not grow an in-process updater.

## Release gate

Before a future release, maintainers should verify the complete Go build/test/vet pipeline,
schema/example parity, adapter corpus and native integration tests available on each runner,
cross-builds for Darwin/Linux/Windows amd64 and arm64, packaging smoke tests, installer checksum
behavior, provenance, container signatures, and documentation/site link checks.

Apple signing preparation is documented in [apple-signing.md](apple-signing.md); Windows signing
preparation is documented in [windows-signing.md](windows-signing.md). Missing native toolchains
must be reported as unexecuted, not treated as native verification by cross-compilation.
