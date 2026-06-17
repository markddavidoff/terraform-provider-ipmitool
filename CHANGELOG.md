# Changelog

All notable changes to this provider will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[0.1.0]: https://github.com/markddavidoff/terraform-provider-ipmitool/releases/tag/v0.1.0
