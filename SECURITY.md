# Security Policy

## Reporting a vulnerability

Please report security vulnerabilities privately via GitHub's
[**Report a vulnerability**](https://github.com/markddavidoff/terraform-provider-ipmitool/security/advisories/new)
button on the Security tab of this repository.

Do not open public issues for unpatched vulnerabilities. Doing so makes
the issue a 0-day to every adopter on the Terraform Registry until a fix
ships.

## Supported versions

| Version | Supported          |
| ------- | ------------------ |
| latest minor (`0.x` series) | ✅ |
| anything else | ❌ |

This is a solo-maintained provider. Only the latest released minor is
patched.

## Provider release signing

All releases are GPG-signed. Verify before trusting downloaded artifacts:

- **Key type:** RSA-4096
- **Fingerprint:** `E594 3F5A C012 46F7 580C  A1AB A513 4C39 9336 2031`
- **Uid:** `Mark Davidoff (terraform-provider-ipmitool release key) <markddavidoff@gmail.com>`

Verify a downloaded release archive:

```bash
gh release download v<version>
gpg --verify SHA256SUMS.sig SHA256SUMS
```

The Terraform Registry verifies the SHA256SUMS signature against the
publisher's key on every `terraform init` of this provider.

## Scope

This policy covers:

- The provider code in this repository.
- The release artifacts published to the Terraform Registry under
  `markddavidoff/ipmitool`.

It does **not** cover:

- Vulnerabilities in the upstream `ipmitool` CLI binary that the
  provider invokes. Report those to the
  [ipmitool project](https://github.com/ipmitool/ipmitool).
- Vulnerabilities in the IPMI protocol itself (RAKP CVE-2013-4786 and
  similar protocol-level issues). These are out of scope and require
  network-level mitigation (isolated management VLAN, strong BMC
  credentials, cipher 17 wherever supported).
