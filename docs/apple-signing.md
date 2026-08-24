# Apple Developer ID and notarization preparation

This is the account-owner checklist for a future signing release. Completing it creates
credentials only; it does not activate a workflow. git-a2a continues to publish unsigned macOS
binaries and its checksum-verifying Homebrew formula until every named secret exists and a
separate reviewed 1.5.x release enables signing.

## 1. Enroll

1. Enroll the publishing identity in the
   [Apple Developer Program](https://developer.apple.com/programs/). Membership is currently
   USD 99 per year or the local-currency equivalent. Enable two-factor authentication first.
2. Use an individual membership only if the public publisher should be the account holder's
   legal personal name. An organization enrollment requires legal authority, the organization's
   legal identity and D-U-N-S number. Apple documents the exact
   [enrollment flow](https://developer.apple.com/help/account/membership/enrolling-in-the-app/).
3. Wait until membership and identity verification are complete before creating certificates.

## 2. Create the Developer ID Application certificate

1. On a Mac, create a Certificate Signing Request in Keychain Access by following Apple's
   [CSR instructions](https://developer.apple.com/help/account/certificates/create-a-certificate-signing-request).
2. As the Account Holder, open Certificates, Identifiers & Profiles, add a certificate, choose
   **Developer ID**, then **Developer ID Application**, upload the CSR, and download the `.cer`.
   Do not choose Developer ID Installer; git-a2a ships command-line executables, not a signed
   installer package. Apple documents the
   [Developer ID certificate flow](https://developer.apple.com/help/account/certificates/create-developer-id-certificates/).
3. Import the `.cer` into the same login keychain that created the CSR. In **My Certificates**,
   export the certificate together with its private key as a password-protected `.p12`.
4. Keep the original `.p12` and its password in an account-owner credential store. Do not commit,
   attach, paste, or log either value.

## 3. Create the notarization API key

1. In App Store Connect, the Account Holder requests App Store Connect API access under
   **Users and Access → Integrations** if it is not already enabled.
2. Create a **Team Key**, name it `git-a2a notarization`, and assign the **Developer** role.
   Do not create an Individual API Key: Apple states that individual keys cannot use
   `notarytool`. The official steps are in
   [Creating API Keys](https://developer.apple.com/documentation/appstoreconnectapi/creating-api-keys-for-app-store-connect-api).
3. Download the `.p8` immediately; Apple offers the private key download only once. Record its
   Key ID and the team's Issuer ID, then store the file securely.

## 4. Create these GitHub environment secrets

Create a protected environment named `apple-signing` and add exactly these secrets:

| Secret | Value |
| --- | --- |
| `APPLE_DEVELOPER_ID_P12_BASE64` | Base64 of the binary `.p12`, emitted without line wrapping |
| `APPLE_DEVELOPER_ID_P12_PASSWORD` | Password chosen when exporting the `.p12` |
| `APPLE_NOTARY_KEY_P8_BASE64` | Base64 of the downloaded `.p8`, emitted without line wrapping |
| `APPLE_NOTARY_KEY_ID` | App Store Connect team key ID |
| `APPLE_NOTARY_ISSUER_ID` | App Store Connect issuer UUID |
| `APPLE_TEAM_ID` | Apple Developer Program Team ID |

Require environment approval by the Account Holder. Report only whether each secret is present;
never print its value. When all six exist, the next task is a separate RC-reviewed implementation
using `rcodesign`, notarization through Apple's Notary API, and a native macOS Gatekeeper smoke
test. Apple explains the security and ticket model in
[Notarizing macOS software](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution).

