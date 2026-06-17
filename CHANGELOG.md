# Changelog

All notable changes to this provider will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.2] — 2026-06-17

**v0.2.2 is the first Registry-published v0.2 release.** Functionally
identical to v0.2.1 except the SBOM regression below.

### Fixed

- **SBOMs removed from the release artifacts.** GoReleaser v2
  unconditionally includes generated `.spdx.json` files in `SHA256SUMS`,
  and there's no `checksum.disable_sbom` flag. The Terraform Registry's
  release-import flow rejects any `SHA256SUMS` entry it doesn't have a
  slot for, so the extra SPDX rows caused both v0.2.0 and v0.2.1 to fail
  Registry indexing with *"missing files in request body"*. Dropping the
  `sboms:` block from `.goreleaser.yml` (and the `Install Syft` step from
  `release.yml`) restores Registry compatibility.
- **v0.2.0 and v0.2.1 GitHub Releases were deleted** because they were
  unusable through the Registry. Their CHANGELOG entries are preserved
  below with a ⚠️ banner so the change history stays auditable. Their
  git tags were also deleted.

### Deferred to v0.3

- Re-introduce SBOMs via a post-GoReleaser step in `release.yml` that
  runs Syft separately and uploads the SBOMs to the release with
  `gh release upload` — keeping them out of `dist/` during the checksum
  step so they don't end up in `SHA256SUMS`.

### Everything else from v0.2.0 + v0.2.1

Apart from the SBOM regression, this release is exactly what v0.2.0 +
v0.2.1 shipped. See the entries below for the full changeset (breaking
changes, security fixes, new features).

## [0.2.1] — 2026-06-17 — ⚠️ BROKEN, never on Registry

Docs-only patch over v0.2.0: rotated the `SECURITY.md` GPG release-key
fingerprint to the v2 key (`CFFA…B507`) so manual signature
verification matches the actual signing key. **Never indexed by the
Terraform Registry** — see v0.2.2 for the SBOM-related root cause. The
GitHub Release was deleted; the tag was deleted.

## [0.2.0] — 2026-06-17 — ⚠️ BROKEN, never on Registry

**Never indexed by the Terraform Registry — see v0.2.2 for the root
cause.** The GitHub Release was deleted; the tag was deleted. The
content below describes what the release would have shipped; v0.2.2 is
functionally identical and is the canonical first v0.2 release.

A consolidation release driven by the three-persona security/UX review
(see `reviews/` in the repo). Closes the four critical findings from
that review, lands every locked-in v0.2 decision (`reviews/decisions.md`),
and ships supply-chain hardening on the release pipeline.

### ⚠️ Breaking changes — read before upgrading

1. **`cipher_suite` is now Required on the provider block** (no default).
   Pick the value per BMC hardware: `17` for iDRAC7+, SuperMicro X10+,
   AsRock Rack; `3` for Dell 11G / iDRAC 6; `0` only with
   `allow_unauthenticated = true` for the no-auth case. Use
   `make detect-cipher HOST=... USER=...` to probe an unknown BMC.
   Migration: add `cipher_suite = <N>` to every `provider "ipmi" {}`
   block.

2. **`force_lockout_risk` attribute is removed.** Replaced by the
   `TF_IPMI_ALLOW_LOCKOUT=1` environment variable on the runner for
   the apply that needs the bypass. The env-var lives only for one
   apply — no sleeper bypass left behind in `.tf`. Every bypass emits
   `tflog.Warn` + a plan-time `Diagnostics.AddWarning`. Migration:
   delete the attribute from HCL; export the env var in the shell
   for the specific apply that needs it.

3. **`ipmi_user.user_password` is now WriteOnly** —
   `user_password` → `user_password_wo` (WriteOnly) +
   `user_password_wo_version` (string trigger). The BMC user-slot
   password is no longer persisted to `terraform.tfstate`. Bump the
   version string to rotate. **Requires Terraform ≥ 1.11** for the
   `ipmi_user` resource specifically; other resources still work on
   Terraform ≥ 1.5.
   Migration:
   ```diff
    resource "ipmi_user" "admin" {
      user_id                  = 2
      name                     = "admin"
   -  user_password            = var.admin_pw
   +  user_password_wo         = var.admin_pw
   +  user_password_wo_version = "1"   # bump to rotate
      privilege                = "administrator"
      enabled                  = true
    }
   ```

4. **`last_updated` attribute removed** from `ipmi_power`, `ipmi_user`,
   `ipmi_lan`, `ipmi_channel_access`, `ipmi_sol`, `ipmi_watchdog`,
   `ipmi_boot_device`, `ipmi_chassis_identify`. The attribute caused a
   no-op diff on every Update; drift detection covers the actual
   state change without it. One apply's worth of state-shape churn on
   upgrade; no data loss.

### Security fixes

- **C-1: BMC password no longer leaks via `ps` / `/proc/<pid>/cmdline`.**
  The provider now hands the password to `ipmitool` via the
  `IPMI_PASSWORD` env var (`-E` flag) instead of `-P` on argv. Verified
  on macOS Homebrew + Debian Bookworm + Alpine ipmitool 1.8.19.

- **C-3 (partial): `ipmi_user` user-slot password no longer in state.**
  WriteOnly attribute (see breaking change #3). Connection-block
  `password` field still persists to state — mitigated by
  state-backend encryption + SOPS / Vault sourcing patterns documented
  in `docs/index.md`.

- **Repo hardening:**
  - `.github/workflows/*.yml`: all GitHub Actions pinned by 40-char
    commit SHA. Closes the `tj-actions/changed-files`-style supply
    chain risk.
  - `.github/dependabot.yml`: weekly updates for `github-actions` and
    `gomod`.
  - `.github/CODEOWNERS`: required reviewer on all PRs and on
    `.github/`, `.goreleaser.yml`, `docs/RELEASING.md`.
  - `SECURITY.md`: GitHub Private Vulnerability Reporting as the
    canonical disclosure channel, RSA-4096 GPG release-key fingerprint
    published.

- **Release pipeline split** (H-8/H-9):
  - `build` job validates the release flow in `--snapshot` mode with
    zero GPG secrets in scope.
  - `sign` job (`needs: build`) imports GPG, runs the real release,
    and attests build provenance via Sigstore (SLSA).
  - SBOMs (SPDX 2.3) published alongside every release zip via Syft.

### New features

- **`ImportState` on all 8 resources.** ID format:
  - `ipmi_power`, `ipmi_boot_device`, `ipmi_chassis_identify`,
    `ipmi_watchdog`, `ipmi_sol`: `<host>:<port>`
  - `ipmi_lan`, `ipmi_channel_access`: `<host>:<port>/ch<channel>`
  - `ipmi_user`: `<host>:<port>/ch<channel>/user<id>`

- **Per-host concurrency cap.** New provider attribute
  `max_concurrent_calls_per_host` (default 3, safe for iDRAC6 session
  table). Raise to 8 for iDRAC7+, 16+ for SuperMicro X10+ / AsRock Rack.

- **Opt-in Configure-time health probe.** New provider attribute
  `health_check = true` runs `mc info` at Configure to fail fast on
  unreachable hosts or bad credentials (provider-block defaults only;
  per-resource overrides still fail at apply time).

- **Structured `tflog` events.**
  - `WARN` on retries (with attempt count, backoff, stderr) and on
    lockout-guard bypasses.
  - `INFO` on every state-changing call (power, user CRUD, LAN, channel
    access, SOL, watchdog, boot device, chassis identify).
  - `DEBUG` on every `ipmitool` invocation.
  - Optional `run_id` provider attribute + `TF_IPMI_RUN_ID` env var
    propagated into every event for SIEM join with CI runner logs.

- **Power resource partial-state recovery.** If `SetPowerState`
  succeeds but the read-back fails, state is written with
  `current_state = "unknown"` instead of erroring — next refresh
  recovers without re-issuing the power transition.

- **`scripts/detect-cipher.sh`** + `make detect-cipher` target —
  probes a BMC for the highest supported RMCP+ cipher suite, with
  explicit warnings about BMC account lockout.

### Docs

- New "Managing BMC credentials" section in `docs/index.md` with
  working examples for SOPS, Vault, and env-var sourcing.
- New "Security & network requirements" section covering management
  VLAN isolation, CVE-2013-4786 (RAKP hash disclosure), state-backend
  expectations, audit-logging story, lockout-guard bypass.
- README adds canonical `source = "markddavidoff/ipmitool"` callout,
  cipher suite hardware-mapping table, honest SuperMicro / AsRock
  "best-effort" disclaimer.

### CI

- `make ci` umbrella target runs `go vet`, `staticcheck`, `go test`,
  `govulncheck`, `gitleaks`, `tfplugindocs validate`.
- `.github/workflows/ci.yml` runs `make ci` on PRs and pushes to main.
- `toolchain go1.26.4` directive added to `go.mod` (closes GO-2026-5037
  and GO-2026-5039 stdlib findings); `golang.org/x/net` bumped to
  v0.55.0 (closes GO-2026-5026).

### Internals

- `ConnectionParams.Port` and `CipherSuite` are now `*int` so an
  explicit zero in a per-resource override is distinguishable from
  "unset".
- `tfplugindocs` invocation pinned to `--provider-name=ipmi` so
  `generate` and `validate` agree on resource doc filenames.

### Deferred to v0.3

- Lockout-guard correctness pass — guards still fire on first-apply
  null-state comparison rather than true change-vs-state-vs-host
  comparison. Tracked as a design question in `reviews/decisions.md`.
- Connection-block `password` WriteOnly — defers until ecosystem
  pattern matures (no comparable provider does this today, per
  Perplexity research recorded in decisions.md Q9.5).
- Active typosquatting monitoring on the Registry.

## [0.1.0] — 2026-06-16

Initial release.

### Resources

- `ipmi_power` — reconcile chassis power state (`on` / `off`) with drift
  detection. Optional `power_off_on_destroy`.
- `ipmi_boot_device` — one-shot or persistent boot device override
  (`pxe`, `disk`, `cdrom`, `bios`, `floppy`, `none`).
- `ipmi_user` — manage IPMI user slots (name / password / privilege /
  enabled). Plan-time **self-disable lockout guard**.
- `ipmi_channel_access` — manage channel-level access mode and auth
  requirements. Plan-time **channel-1-disable lockout guard**.
- `ipmi_lan` — manage LAN config (IP / netmask / gateway / VLAN / RMCP
  port / DHCP). Plan-time **BMC IP / DHCP / VLAN change lockout guards**.
- `ipmi_watchdog` — configure the IPMI watchdog timer (timeout, action,
  log, stopped).
- `ipmi_chassis_identify` — blink the chassis identify LED.
- `ipmi_sol` — manage Serial-over-LAN config (enabled, bitrate,
  privilege limit, force-auth/encryption).

### Data sources

- `ipmi_chassis_status` — power state + fault indicators.
- `ipmi_bmc_info` — BMC firmware and manufacturer (from `mc info`).
- `ipmi_fru` — FRU inventory (board / chassis / product serials).
- `ipmi_sel` — System Event Log records (last N entries).
- `ipmi_sensors` — sensor data records (temps / fans / voltages).

### Verified hardware

- Dell PowerEdge R210 II, bare integrated BMC, firmware 1.90, cipher
  suite 3 (the original Dell 11G target).
- Dell iDRAC 7 Enterprise, firmware 2.21, cipher suite 3.

### Known limitations

- **Dell 11G bare BMC rejects remote `Set User Name`** — `ipmi_user`
  works on conforming BMCs (SuperMicro, AsRock Rack, etc.) but Dell 11G
  requires RACADM for user CRUD.
- **iDRAC6 session table is small** — five parallel data sources can
  exceed it. The provider transparently retries on `insufficient
  resources for session` errors.
- **`ipmitool sdr list` is slow on iDRAC6** (~33 s). Default
  `timeout_seconds` is 60.

[0.2.2]: https://github.com/markddavidoff/terraform-provider-ipmitool/releases/tag/v0.2.2
[0.2.1]: https://github.com/markddavidoff/terraform-provider-ipmitool/releases/tag/v0.2.1
[0.2.0]: https://github.com/markddavidoff/terraform-provider-ipmitool/releases/tag/v0.2.0
[0.1.0]: https://github.com/markddavidoff/terraform-provider-ipmitool/releases/tag/v0.1.0
