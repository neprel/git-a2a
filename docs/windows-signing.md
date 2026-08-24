# Windows Authenticode options

Decision owner: Andrey. No release workflow is changed until one option is selected and its
publisher identity is reviewed. Today the GitHub Release `.exe` files are unsigned: checksums and
GitHub provenance prove their source and bytes, but Windows can still display an unknown-publisher
or SmartScreen warning.

| | Azure Artifact Signing | Traditional OV certificate |
| --- | --- | --- |
| Custody | Microsoft-managed short-lived certificate; no private key or hardware token in CI | CA-issued identity; current rules require the private key in an HSM, cloud HSM, or hardware token |
| CI fit | Native service credentials and a GitHub Actions integration | Depends on CA/token; unattended CI usually needs a cloud-HSM signing service |
| Current indicative cost | Basic: USD 9.99/account/month, 5,000 signatures included | Microsoft estimates USD 150–300/year; verify a current quote from the selected CA |
| Availability | Organizations in USA, Canada, EU, UK; individuals in USA/Canada | Worldwide, subject to the CA's identity-verification policy |
| Identity work | Azure subscription, identity validation, Public Trust certificate profile | CA organization validation and key-custody enrollment |
| SmartScreen | Signed publisher is visible, but reputation still accumulates | Same reputation model; initial warnings can still occur |
| Operations | Best fit for a small GitHub Actions release pipeline when geography permits | Better when Azure is unavailable or a customer requires a particular CA |

Microsoft's current guidance recommends
[Artifact Signing for non-Store distribution](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/code-signing-options)
and documents the [Basic/Premium quotas and prices](https://learn.microsoft.com/en-us/azure/artifact-signing/how-to-change-sku).
It also states that OV and EV certificates no longer bypass SmartScreen immediately: a new signed
publisher/file still builds reputation. See
[SmartScreen reputation](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation).

## Recommendation

Choose Azure Artifact Signing Basic if the publishing identity is eligible in a supported region.
It is the least operationally complex CI option and avoids introducing a long-lived signing key.
Choose an OV certificate only if geography, legal identity, or an enterprise requirement rules
Azure out; compare CA quotes including the mandatory HSM/cloud-signing service, not only the
certificate headline price. EV is not recommended solely for SmartScreen because Microsoft says
it no longer receives an instant reputation advantage.

After Andrey chooses, the implementation must sign both Windows architectures before archives are
created, timestamp the signatures, verify them with native Windows tooling, and pass a new RC plus
written acceptance. The reserved integration point is the GoReleaser build/archive boundary; this
document does not activate it.

