terraform {
  required_providers {
    ipmi = {
      source  = "markddavidoff/ipmitool"
      version = "~> 0.1"
    }
  }
}

variable "ipmi_host" { type = string }
variable "ipmi_user" { type = string }
variable "ipmi_pass" {
  type      = string
  sensitive = true
}

provider "ipmi" {
  host         = var.ipmi_host
  username     = var.ipmi_user
  password     = var.ipmi_pass
  cipher_suite = 3
}

# --- read-only ---
data "ipmi_chassis_status" "r210ii" {}
data "ipmi_bmc_info" "r210ii" {}
data "ipmi_fru" "r210ii" {}
data "ipmi_sensors" "r210ii" {}
data "ipmi_sel" "r210ii" {
  max_entries = 5
}

# --- power resource ---
# state = "on" matches the host's current state, so this is a no-op
# write that exercises the full Create → Read → drift-detection loop
# without actually flipping anything.
resource "ipmi_power" "r210ii" {
  state                = "on"
  power_off_on_destroy = false
}

output "power_on" {
  value = data.ipmi_chassis_status.r210ii.power_on
}

output "bmc_info" {
  value = {
    firmware_version  = data.ipmi_bmc_info.r210ii.firmware_version
    manufacturer_name = data.ipmi_bmc_info.r210ii.manufacturer_name
    ipmi_version      = data.ipmi_bmc_info.r210ii.ipmi_version
  }
}

output "fru" {
  value = {
    board_mfg     = data.ipmi_fru.r210ii.board_mfg
    board_product = data.ipmi_fru.r210ii.board_product
    board_serial  = data.ipmi_fru.r210ii.board_serial
    product_name  = data.ipmi_fru.r210ii.product_name
  }
}

output "sensor_count" {
  value = length(data.ipmi_sensors.r210ii.sensors)
}

output "first_temp_sensor" {
  # find the first sensor whose unit looks like a temp
  value = try([for s in data.ipmi_sensors.r210ii.sensors : s if can(regex("degrees", s.unit))][0], null)
}

output "sel_count" {
  value = length(data.ipmi_sel.r210ii.entries)
}

output "power_resource" {
  value = {
    id            = ipmi_power.r210ii.id
    desired       = ipmi_power.r210ii.state
    current       = ipmi_power.r210ii.current_state
    last_updated  = ipmi_power.r210ii.last_updated
  }
}
