// Mixed-fleet example.
//
// Demonstrates managing N BMCs from one provider block using a `locals.hosts`
// map and `for_each`. Per-resource `host` / `username` / `password` attributes
// override the provider's defaults, so a single ipmi provider serves the whole
// fleet.
//
// Aimed at the home-server use case (R210 II + 2× R220) but the pattern scales
// to any number of BMCs of any vendor.

terraform {
  required_providers {
    ipmi = {
      source  = "markddavidoff/ipmitool"
      version = "~> 0.1"
    }
  }
}

// `fleet` is intentionally NOT sensitive — Terraform forbids sensitive
// values from being used as for_each keys/values. Passwords are split into
// a separate sensitive map keyed by the same alias.
variable "fleet" {
  description = "Map of host alias → BMC connection metadata (no secrets)."
  type = map(object({
    host         = string
    username     = string
    cipher_suite = optional(number, 3) // R210 II / Dell 11G default
  }))
}

variable "fleet_passwords" {
  description = "Map of host alias → BMC password. Sourced from a secret manager (sops, 1Password, etc.) via TF_VAR_fleet_passwords."
  type        = map(string)
  sensitive   = true
}

provider "ipmi" {
  // Defaults are unset; each resource provides its own connection params.
  // This style keeps the provider block tiny and the fleet map authoritative.
  cipher_suite = 3
}

// ──────────── Read-only monitoring ────────────

data "ipmi_chassis_status" "fleet" {
  for_each     = var.fleet
  host         = each.value.host
  username     = each.value.username
  password     = var.fleet_passwords[each.key]
  cipher_suite = each.value.cipher_suite
}

data "ipmi_bmc_info" "fleet" {
  for_each     = var.fleet
  host         = each.value.host
  username     = each.value.username
  password     = var.fleet_passwords[each.key]
  cipher_suite = each.value.cipher_suite
}

data "ipmi_fru" "fleet" {
  for_each     = var.fleet
  host         = each.value.host
  username     = each.value.username
  password     = var.fleet_passwords[each.key]
  cipher_suite = each.value.cipher_suite
}

// ──────────── Declarative power state ────────────

resource "ipmi_power" "fleet" {
  for_each     = var.fleet
  host         = each.value.host
  username     = each.value.username
  password     = var.fleet_passwords[each.key]
  cipher_suite = each.value.cipher_suite

  state                = "on"
  power_off_on_destroy = false
}

// ──────────── Aggregated outputs ────────────

output "fleet_summary" {
  description = "Per-host power state + identity for quick monitoring."
  value = {
    for k, h in var.fleet : k => {
      host           = h.host
      power_on       = data.ipmi_chassis_status.fleet[k].power_on
      bmc_firmware   = data.ipmi_bmc_info.fleet[k].firmware_version
      board_product  = data.ipmi_fru.fleet[k].board_product
      board_serial   = data.ipmi_fru.fleet[k].board_serial
      power_resource = ipmi_power.fleet[k].id
    }
  }
}

output "all_powered_on" {
  description = "True when every host in the fleet reports power_on = true."
  value       = alltrue([for s in data.ipmi_chassis_status.fleet : s.power_on])
}

output "any_intrusion_detected" {
  description = "True when any host reports an active chassis intrusion."
  value       = anytrue([for s in data.ipmi_chassis_status.fleet : s.chassis_intrusion])
}
