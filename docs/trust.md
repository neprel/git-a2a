# Trust

git-a2a can bind a fetched agent card to an expected key and origin, and a locked Git object to
the consumer's signer policy. These checks are opt-in: manifests without `trust` or `require`
keep the ordinary Git-and-hash workflow.

## Threats and controls

| Threat | Control |
|---|---|
| A card supplies its own attacker-controlled JWKS | Pin `agents[].trust.jwks` or RFC 7638 `keys`; an unpinned fallback must share the card origin and is reported as unpinned. |
| A card or interface is hosted by a look-alike owner | Pin `trust.origins`, or rely on the repository-origin / git-a2a extension-repository binding. |
| A locked commit was not made by an allowed owner | Set `dependencies[].require.commits: signed` and a repository-relative Git `allowed_signers` file. |
| A verified card rotates to an unexpected key | The lock records `kid` and thumbprint; `status` fails until `update --accept-keys`. |
| A cached JWKS keeps a revoked key indefinitely | Online checks refresh after `jwks-max-age` (24h by default); a key absent from a fresh set no longer verifies. |
| An owner declined requests from another organisation | `contact` refuses when `accepts-external: false`; only a human CLI call may use `--external-ok`. |
| Dependency text tries to instruct an agent | Markdown is fenced and sanitised; JSON/MCP names dependency data in `untrustedFields`. |

## Enable it in ten minutes

1. Publish a JWKS over HTTPS and add its URL to each card-owning agent's `trust.jwks` (or pin
   individual RFC 7638 thumbprints in `trust.keys`). Set `signatures: true` and sign the A2A card
   as described in the [authoring guide](authoring.md).
2. Generate an SSH signing key, configure Git's `gpg.format ssh` and `user.signingKey`, and sign
   commits or annotated tags. Commit only the public key in a Git `allowed_signers` file.
3. In the consumer, declare:

   ```yaml
   dependencies:
     - id: acme-lib
       git: https://github.com/acme/lib
       ref: main
       require:
         commits: signed
         signers: trust/allowed_signers
         cards: signed
         card-origin: true
   ```

4. Run `git-a2a update --review`, inspect any key or card changes, then run `git-a2a trust show`
   and `git-a2a status -v`. Accept a deliberate key rotation with `update --accept-keys`.

For a one-off incident, `add`, `update`, `set`, and `fetch` accept
`--insecure-skip-signers`; lock-writing commands record `verified: skipped`. Treat that state as
an unresolved exception, not successful verification.

## Origin and organisation rules

Explicit `trust.origins` values govern both the card URL and every
`supportedInterfaces[].url`. Without them, the URL origin must match `module.repository`, unless
the card's git-a2a extension names the same canonical Git repository as the dependency.

An organisation defaults to repository host plus its first path segment, such as
`github.com/acme`. `settings.organisation` can list equivalent organisation prefixes. This value
is used only to enforce `accepts-external`; it is not an identity federation system.

## What this does not protect

- It does not authenticate an A2A request; use the card's A2A security schemes.
- It does not make a signing key trustworthy by itself. The consumer must review how a JWKS,
  thumbprint, or `allowed_signers` file was obtained.
- It does not protect code after a package manager or compiler has fetched additional artifacts.
- It does not provide a central identity registry, certificate authority, daemon, or key server.
- It does not sign git-a2a release binaries beyond the release checksums and existing package
  channel verification.

See the generated [manifest reference](manifest-reference.md), [consumer guide](consuming.md),
and [CLI reference](cli.md#trust) for exact fields and commands.
