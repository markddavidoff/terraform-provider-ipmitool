# Releasing terraform-provider-ipmitool

End-to-end checklist for cutting a release that lands on the
[Terraform Registry](https://registry.terraform.io/).

## One-time setup

### 1. Generate a GPG release key

The Registry requires releases to be signed with a GPG key whose public
key is registered on your account.

> **Important:** the Registry rejects ECC keys (GPG's interactive
> default in recent versions). The batch config below pins `Key-Type:
> RSA` so you don't accidentally generate an unusable key.

Generate a strong random passphrase, then the key:

```bash
# 1. Random passphrase, mode-600 so it doesn't leak via $HISTFILE.
mkdir -p ~/.gnupg/release-key-bootstrap
chmod 700 ~/.gnupg/release-key-bootstrap
umask 077
openssl rand -base64 32 | tr -d '\n' \
  > ~/.gnupg/release-key-bootstrap/passphrase.txt

# 2. Generate the key, pinned to RSA-4096 with a dedicated identity.
PASS=$(cat ~/.gnupg/release-key-bootstrap/passphrase.txt)
cat > ~/.gnupg/release-key-bootstrap/keygen.conf <<EOF
Key-Type: RSA
Key-Length: 4096
Name-Real: Mark Davidoff
Name-Comment: terraform-provider-ipmitool release key
Name-Email: markddavidoff@gmail.com
Expire-Date: 0
Passphrase: $PASS
%commit
EOF
chmod 600 ~/.gnupg/release-key-bootstrap/keygen.conf
gpg --batch --generate-key ~/.gnupg/release-key-bootstrap/keygen.conf
rm ~/.gnupg/release-key-bootstrap/keygen.conf
```

Why a passphrase: a passphrase-less key means the `GPG_PRIVATE_KEY`
GitHub Actions secret IS the full key — anything that exfiltrates the
secret can sign forged releases. With a passphrase, an attacker needs
both `GPG_PRIVATE_KEY` and `PASSPHRASE`. In the CI runtime attack
model these are coupled (both load into the same job's env), but the
passphrase materially helps against leaks via other channels (lost
laptop, accidental gist, cross-CI-platform migration).

**Save the passphrase to your password manager immediately**:

```bash
cat ~/.gnupg/release-key-bootstrap/passphrase.txt
# → copy → 1Password / pass / Keychain
```

Get the fingerprint:

```bash
gpg --list-secret-keys --keyid-format=long
```

Export the public key (you'll paste this on the Registry):

```bash
gpg --armor --export <FINGERPRINT> > ~/.gnupg/release-key-bootstrap/public-key.asc
```

Export the passphrase-encrypted private key (for the GH Actions secret):

```bash
gpg --batch --pinentry-mode loopback \
    --passphrase "$(cat ~/.gnupg/release-key-bootstrap/passphrase.txt)" \
    --armor --export-secret-keys <FINGERPRINT> \
  > ~/.gnupg/release-key-bootstrap/private-key.asc
```

### 2. Register the public key on the Terraform Registry

1. Sign in to https://registry.terraform.io/sign-in (GitHub OAuth).
2. https://registry.terraform.io/settings/gpg-keys → New GPG Signing Key.
3. Paste the contents of `~/.gnupg/release-key-bootstrap/public-key.asc`.

### 3. Make the repo public

Registry publishing requires a public GitHub repo:

```bash
gh repo edit markddavidoff/terraform-provider-ipmitool --visibility public
```

### 4. Add CI secrets

Set both directly from the bootstrap files (no copy-paste of secret material):

```bash
REPO=markddavidoff/terraform-provider-ipmitool
gh secret set GPG_PRIVATE_KEY --repo $REPO \
  < ~/.gnupg/release-key-bootstrap/private-key.asc
gh secret set PASSPHRASE      --repo $REPO \
  < ~/.gnupg/release-key-bootstrap/passphrase.txt
gh secret list --repo $REPO   # confirm both present
```

### 5. Shred the bootstrap dir

The key is now in your local `~/.gnupg/` keyring and on GitHub as
secrets. The bootstrap dir served its purpose; shred it:

```bash
find ~/.gnupg/release-key-bootstrap -type f -exec sh -c \
  'dd if=/dev/urandom of="$1" bs=1024 count=8 2>/dev/null; rm -f "$1"' _ {} \;
rmdir ~/.gnupg/release-key-bootstrap
```

To re-export the public key later (e.g., for a different repo):
`gpg --armor --export <FINGERPRINT>`.

### 6. Publish the provider on the Registry

1. https://registry.terraform.io/publish/provider
2. Select `markddavidoff/terraform-provider-ipmitool`.
3. Category: **Infrastructure (IaaS)** — matches `dell/redfish`,
   `equinix/metal`, `tinkerbell/tinkerbell`.
4. The Registry watches for `v*` tags and pulls release artifacts.

## Per-release flow

### Option A — release via GitHub Actions (recommended)

```bash
# Convenience wrapper: validates we're on main / in sync / no tag
# collision, prompts for the GPG passphrase silently (no pinentry —
# avoids the "Inappropriate ioctl for device" failure mode), signs +
# pushes, and offers to start `gh run watch`. The release-key
# fingerprint is hard-coded in the script.
make release-tag VERSION=v0.3.0

# Or, the manual equivalent:
git tag -s -u <fingerprint> v0.3.0 -m "v0.3.0"
git push origin v0.3.0
```

The `.github/workflows/release.yml` workflow takes it from there
(builds the 12 OS/arch matrix, signs `SHA256SUMS`, uploads the
release). Typically completes in ~5 minutes.

Watch the run:

```bash
gh run watch --repo markddavidoff/terraform-provider-ipmitool \
  --workflow release.yml --exit-status
```

### Option B — release locally

```bash
# 1. Make sure tests are green.
make test
make testacc          # if you have a BMC handy

# 2. Tag + push.
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0

# 3. Run GoReleaser. The GPG agent will prompt for the passphrase
#    unless you've loopback-imported the key non-interactively.
export GPG_FINGERPRINT=<your fingerprint>
goreleaser release --clean
```

## What gets shipped

GoReleaser builds and uploads to a GitHub Release:

- `terraform-provider-ipmitool_0.1.0_<os>_<arch>.zip` for each
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
