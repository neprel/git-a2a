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

## Verification

Tag-triggered release assets carry GitHub build provenance. After downloading an asset:

```sh
gh attestation verify PATH/TO/ASSET \
  --repo neprel/git-a2a \
  --signer-workflow neprel/git-a2a/.github/workflows/release.yml
sha256sum --ignore-missing -c checksums.txt
```

The GHCR image is signed keylessly at its immutable digest. Verify the GitHub OIDC issuer and the
tag-triggered release workflow identity:

```sh
cosign verify \
  --certificate-identity-regexp '^https://github\.com/neprel/git-a2a/\.github/workflows/release\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$' \
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
