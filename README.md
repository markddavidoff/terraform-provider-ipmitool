# terraform-provider-ipmitool

A Terraform provider that orchestrates BMC hardware over IPMI 2.0 (LAN+)
by wrapping the [ipmitool](https://github.com/ipmitool/ipmitool) CLI.

Targets the homelab use case: declaratively manage power state, boot
device, BMC users, channel access, and LAN config across a mixed fleet
of Dell PowerEdge servers (and any other IPMI 2.0 BMC). Built primarily
to fill the gap where Dell's own
[terraform-provider-redfish](https://github.com/dell/terraform-provider-redfish)
doesn't reach — older Dell 11G hardware (R210 II, R610, R710) that
predates Redfish.

> **Status:** v0.1.0. Verified end-to-end against a Dell R210 II bare
> BMC and a Dell iDRAC 7 Enterprise.

## Why a new provider

The IPMI/BMC corner of homelab + datacenter automation is
under-served in Terraform:

- `dell/terraform-provider-redfish` only supports iDRAC7+ — useless for
  the millions of still-running Dell 11G servers.
- `bmc-toolbox/bmclib` is a Go library, not a Terraform provider, and is
  oriented toward Tinkerbell-style bare-metal provisioning.
- Existing "Option B"-style providers wrap `ipmitool` via `local-exec`,
  which has no state management.

This provider is a thin idiomatic Terraform layer over `ipmitool` with
proper drift detection, lockout-safety guards, and per-resource
connection overrides for multi-host fleets.

## Requirements

- Terraform ≥ 1.5 (or OpenTofu). The `ipmi_user` resource specifically
  requires Terraform ≥ 1.11 for its WriteOnly password attribute.
- `ipmitool` ≥ 1.8.18 installed on the host that runs `terraform apply`
  - macOS: `brew install ipmitool`
  - Debian / Ubuntu: `apt install ipmitool`
  - Alpine: `apk add ipmitool`
  - Windows: install WSL2, then `apt install ipmitool` inside WSL2

The provider detects `ipmitool` at `Configure` time and emits a clear
install hint if it isn't on `PATH`.

> Canonical Registry source: **`markddavidoff/ipmitool`**. Beware
> typo-squat namespaces — if your `source =` line is anything else,
> it's not this provider.

## Cipher suite selection

`cipher_suite` is **required** on the provider block — no safe default.
The right value depends on your BMC hardware:

| Hardware                  | cipher_suite |
|---------------------------|--------------|
| Dell PowerEdge 11G (bare) | `3`          |
| Dell iDRAC 6              | `3`          |
| Dell iDRAC 7+             | `17`         |
| SuperMicro X10/X11/X12    | `17`         |
| AsRock Rack               | `17`         |
| Unknown                   | run `make detect-cipher` |

Cipher `3` is RAKP-HMAC-SHA1 + AES-CBC-128. Cipher `17` is
RAKP-HMAC-SHA256 + AES-CBC-128. Use `17` wherever supported.

⚠️ **Lockout warning when probing:** the `detect-cipher` script makes
one auth attempt per cipher suite tried. Most BMCs lock the account
after 3–5 failed attempts (iDRAC default: 3 → 10-min lockout). Run
against ONE host at a time, with a known-good username/password.

## Quick start

```hcl
terraform {
  required_providers {
    ipmi = {
      source  = "markddavidoff/ipmitool"
      version = "~> 0.2"
    }
  }
}

provider "ipmi" {
  host         = "192.0.2.10"
  username     = "root"
  password     = var.bmc_password
  cipher_suite = 17                # see "Cipher suite selection" above
}

# Read current chassis state.
data "ipmi_chassis_status" "host" {}

# Reconcile power state with drift detection.
resource "ipmi_power" "host" {
  state                = "on"
  power_off_on_destroy = false     # safer default
}

output "power_state" {
  value = data.ipmi_chassis_status.host.power_on
}
```

## Managing BMC credentials

`password = var.bmc_password` in a `.tfvars` is the obvious
starting point and the wrong long-term default — BMC creds end up in
state. Source them through SOPS, Vault, or env vars instead. See
[Managing BMC credentials in the provider docs](docs/index.md#managing-bmc-credentials)
for working examples.

## Security & network requirements

IPMI must live on an isolated management VLAN. The Terraform runner
needs UDP/623 to each BMC; nothing else should. Use cipher 17 wherever
supported, keep BMC passwords ≥ 20 chars, and treat state-file access
as equivalent to BMC root. See
[Security & network requirements in the provider docs](docs/index.md#security--network-requirements)
for the full threat model including CVE-2013-4786 (RAKP hash
disclosure).

## Resources

| Resource | Purpose |
|---|---|
| `ipmi_power` | Reconcile chassis power state (`on` / `off`). Idempotent. |
| `ipmi_boot_device` | One-shot or persistent boot device override (`pxe`, `disk`, `cdrom`, `bios`, `floppy`, `none`). |
| `ipmi_user` | Manage IPMI user slots (name / password / privilege / enabled). Self-disable lockout guard. |
| `ipmi_channel_access` | Manage channel-level access mode + auth requirements. Channel-1-disable lockout guard. |
| `ipmi_lan` | Manage LAN config: IP / subnet / gateway / VLAN / RMCP port. IP/DHCP/VLAN lockout guards. |
| `ipmi_watchdog` | Configure the IPMI watchdog timer (timeout, action, log, stopped). |
| `ipmi_chassis_identify` | Blink the chassis identify LED for spot-locating hosts in a rack. |
| `ipmi_sol` | Manage Serial-over-LAN config (enabled, bitrate, privilege limit, force-auth/encryption). |

## Data sources

| Data source | Purpose |
|---|---|
| `ipmi_chassis_status` | Power state + fault indicators. |
| `ipmi_bmc_info` | BMC firmware + manufacturer ID (parsed from `mc info`). |
| `ipmi_fru` | Field-Replaceable Unit inventory (board / chassis / product serial). |
| `ipmi_sel` | System Event Log records (last N entries). |
| `ipmi_sensors` | All Sensor Data Records (temps / fans / voltages). |

## Lockout safety

Three resources (`ipmi_user`, `ipmi_channel_access`, `ipmi_lan`) include
plan-time lockout guards that block self-destructive applies:

- Disabling the connection user
- Disabling LAN access on channel 1
- Changing the BMC's IP, switching to DHCP, or changing VLAN on channel 1

Each blocked plan errors with a clear message and the remedy: set
`TF_IPMI_ALLOW_LOCKOUT=1` in the runner environment to bypass the
guard for that apply. The bypass is operational (per-apply) rather
than declarative (in `.tf`), so it can't be accidentally left set
across runs. Every bypass emits a `tflog.Warn` for the SIEM trail.

## Multi-host fleets

Per-resource connection attributes override the provider defaults, so a
single `provider "ipmi" {}` block can manage N BMCs:

```hcl
locals {
  fleet = {
    r210ii  = { host = "192.0.2.10", username = "root" }
    r220-a  = { host = "192.0.2.11", username = "root" }
    r220-b  = { host = "192.0.2.12", username = "root" }
  }
}

resource "ipmi_power" "fleet" {
  for_each = local.fleet
  host     = each.value.host
  username = each.value.username
  password = var.fleet_passwords[each.key]
  state    = "on"
}
```

See [`examples/mixed-fleet/`](examples/mixed-fleet/) for a complete
working example.

## Verified hardware

Tested end-to-end against:

- **Dell PowerEdge R210 II** with the bare integrated BMC (no iDRAC
  Express/Enterprise card), firmware 1.90, cipher suite 3.
- **Dell iDRAC 7 Enterprise**, firmware 2.21, cipher suite 3.

Known limitations:

- **Non-Dell hardware is best-effort.** The `splitColumns` parser in
  `client_ipmitool.go` was verified against Dell R210 II and iDRAC 7
  output formats only. SuperMicro X9/X10/X11 and AsRock Rack BMCs ship
  the same upstream `ipmitool` but vendor BMC firmware varies in
  column layout for `user list` and similar. If you hit parse failures
  on non-Dell hardware, open an issue with the raw output of
  `ipmitool user list 1` / `ipmitool lan print 1` and we'll add a
  fixture.
- **Dell 11G bare BMC rejects remote `Set User Name`** — the
  `ipmi_user` resource works correctly on conforming BMCs (SuperMicro,
  AsRock Rack, etc.) but Dell 11G requires RACADM for user CRUD.
- **iDRAC6 session table is small** — five parallel data sources can
  exceed it. The provider transparently retries on `insufficient
  resources for session` errors.
- **`ipmitool sdr list` is slow on iDRAC6** (~33s). Default
  `timeout_seconds` is 60.

## Development

```bash
make build         # build the provider binary
make test          # unit tests (no external deps)
make testacc       # acceptance tests against the secret-stored BMC
make install-local # install to ~/.terraform.d for local testing
```

Acceptance tests use [sops](https://github.com/getsops/sops) +
[age](https://github.com/FiloSottile/age) to keep BMC credentials out of
the repo. `make secrets-set-one KEY=IPMI_HOST` to populate.

## License

[MPL-2.0](LICENSE) — matches HashiCorp's convention for Terraform providers.
