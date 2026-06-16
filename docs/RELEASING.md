# Releasing terraform-provider-ipmitool

End-to-end checklist for cutting a release that lands on the
[Terraform Registry](https://registry.terraform.io/).

## One-time setup

### 1. Generate a GPG release key

The Registry requires releases to be signed with a GPG key whose public
key is registered on your account.

```bash
gpg --full-generate-key \
    --batch <<EOF
%no-protection
Key-Type: RSA
Key-Length: 4096
Name-Real: Mark Davidoff
Name-Email: markddavidoff@gmail.com
Expire-Date: 0
%commit
EOF
```

(Adjust name/email. `%no-protection` makes the key passphrase-less,
which is friendlier for CI; if you'd rather use a passphrase, remove
that line and set the `PASSPHRASE` repo secret.)

Get the fingerprint:

```bash
gpg --list-secret-keys --keyid-format=long
```

Export the public key:

```bash
gpg --armor --export <FINGERPRINT> > release-key.pub
```

### 2. Register the public key on Terraform Cloud

1. Sign in to https://app.terraform.io/.
2. Settings → User Profile → GPG Signing Keys → New GPG Signing Key.
3. Paste the contents of `release-key.pub`.

### 3. Make the repo public

Registry publishing requires a public GitHub repo:

```bash
gh repo edit markddavidoff/terraform-provider-ipmitool --visibility public
```

### 4. Add CI secrets (if releasing via GitHub Actions)

In the repo → Settings → Secrets and variables → Actions → New repository secret:

- `GPG_PRIVATE_KEY` — `gpg --armor --export-secret-keys <FINGERPRINT>`
- `PASSPHRASE` — empty string if you used `%no-protection` above

### 5. Publish the provider on the Registry

1. https://registry.terraform.io/publish/provider
2. Pick the GitHub repo `markddavidoff/terraform-provider-ipmitool`.
3. The Registry watches for `v*` tags and pulls release artifacts.

## Per-release flow

### Option A — release locally

```bash
# 1. Make sure tests are green.
make test
make testacc          # if you have a BMC handy

# 2. Tag.
git tag -s v0.1.0 -m "v0.1.0"
git push origin v0.1.0

# 3. Run GoReleaser.
export GPG_FINGERPRINT=<your fingerprint>
goreleaser release --clean
```

### Option B — release via GitHub Actions

```bash
git tag -s v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

The `.github/workflows/release.yml` workflow takes it from there.

## What gets shipped

GoReleaser builds and uploads to a GitHub Release:

- `terraform-provider-ipmitool_v0.1.0_<os>_<arch>.zip` for each
  freebsd/windows/linux/darwin × amd64/arm/arm64/386 combo
  (with some exclusions — darwin/386 and darwin/arm don't exist)
- `terraform-provider-ipmitool_0.1.0_SHA256SUMS` — checksums of every zip
- `terraform-provider-ipmitool_0.1.0_SHA256SUMS.sig` — GPG signature of the sums
- `terraform-provider-ipmitool_0.1.0_manifest.json` — copy of `terraform-registry-manifest.json`

The Registry verifies the signature against your registered public key,
then makes the provider installable as
`source = "markddavidoff/ipmitool"` in any `required_providers` block.

## Validating before tagging

Dry-run the release locally to catch config errors:

```bash
goreleaser release --snapshot --clean --skip=sign
```

This builds all the artifacts in `dist/` without trying to sign or upload.
