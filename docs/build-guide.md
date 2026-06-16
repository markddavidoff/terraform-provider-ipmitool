# terraform-provider-ipmitool: Build Guide

## Overview

This document captures the full context needed to build a native Terraform provider for IPMI-based BMC management, with primary motivation being management of a **Dell R210 II (iDRAC6)** alongside iDRAC7 servers in a unified Terraform workflow.

**Why this is needed:** Dell's official `terraform-provider-redfish` only supports iDRAC7 and above, and no maintained Terraform provider exists for IPMI 2.0 BMC control of legacy hardware. The `bmc-toolbox/bmclib` project is a Go library only, not a Terraform provider. The only Terraform-native options for iDRAC6 are `local-exec` wrappers around `ipmitool`, which offer no state management.[^1][^2][^3]

***

## Background: Protocol Support Matrix

| Protocol | iDRAC6 (R210 II) | iDRAC7 | Notes |
|---|---|---|---|
| Redfish REST API | ❌ No | ✅ Yes (limited) | iDRAC7 Redfish has smaller feature set than iDRAC8/9[^4] |
| IPMI 2.0 | ✅ Yes | ✅ Yes | Full OOB power/sensor/boot control |
| WS-MAN | ✅ Yes | ✅ Yes | No Terraform provider exists for it |
| RACADM CLI | ✅ Yes | ✅ Yes | SSH-based; scriptable via `local-exec` |

Dell confirmed iDRAC6 has no Redfish support. The `dell/terraform-provider-redfish` minimum requirement is iDRAC9 firmware ≥ 5.0 for full feature support, and iDRAC7/8 support is also limited vs. iDRAC9.[^4][^5][^6]

***

## Architecture Decision: Native Go vs. CLI Wrapper

Two approaches exist for the IPMI protocol layer inside the provider:

### Option A: Pure Go — `bougou/go-ipmi` (Recommended)
`go-ipmi` is a native Go IPMI v1.5/v2.0 client library that does **not** wrap `ipmitool` or any external binary. It implements the full IPMI command set including chassis control, sensor data records (SDR), system event log (SEL), and user management.[^7][^8]

```go
import "github.com/bougou/go-ipmi"

client, err := ipmi.NewClient("192.0.2.50", 623, "root", "calvin")
if err != nil { ... }
client.WithInterface(ipmi.InterfaceLanplus)
if err = client.Connect(); err != nil { ... }
res, err := client.GetChassisStatus()
```

**Advantages:** Single binary distribution, no runtime dependency on `ipmitool`, fully testable with interface mocking, idiomatic Go.

### Option B: CLI Wrapper — `squarefactory/ipmitool`
A Go wrapper around the `ipmitool` binary. Easier initial implementation but requires `ipmitool` installed on every Terraform host — a runtime dependency that complicates distribution and CI.[^9]

**Recommendation:** Use `bougou/go-ipmi` for Option A. The pure Go approach is the standard for Terraform providers (no provider bundles external binaries).

***

## Terraform Plugin Framework

All new providers should be built with HashiCorp's **Terraform Plugin Framework** (not the older SDK). It provides:[^10][^11]

- Type-safe schema definitions
- Built-in diagnostics system
- gRPC-based protocol handling (automatic)
- First-class support for `terraform-plugin-testing`

**Required Go modules:**

```
github.com/hashicorp/terraform-plugin-framework
github.com/hashicorp/terraform-plugin-framework/providerserver
github.com/hashicorp/terraform-plugin-testing
github.com/bougou/go-ipmi
```

***

## Project Structure

```
terraform-provider-ipmitool/
├── main.go
├── go.mod
├── go.sum
├── internal/
│   ├── provider/
│   │   ├── provider.go          # Provider schema + client init
│   │   ├── provider_test.go
│   ├── ipmi/
│   │   ├── client.go            # Thin wrapper/interface over bougou/go-ipmi
│   │   ├── client_mock.go       # Mock implementation for unit tests
│   ├── resources/
│   │   ├── power_resource.go
│   │   ├── power_resource_test.go
│   │   ├── boot_device_resource.go
│   │   ├── boot_device_resource_test.go
│   │   ├── user_resource.go
│   ├── datasources/
│   │   ├── sensors_datasource.go
│   │   ├── chassis_datasource.go
├── examples/
│   ├── basic/main.tf
│   ├── mixed-fleet/main.tf      # iDRAC6 IPMI + iDRAC7 Redfish side by side
├── docs/
│   ├── resources/
│   ├── data-sources/
├── .github/
│   └── workflows/
│       ├── test.yml
│       └── release.yml          # GoReleaser for registry publishing
```

***

## Provider Schema

```go
// internal/provider/provider.go
func (p *ipmiProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
    resp.Schema = schema.Schema{
        Description: "Manage BMC/iDRAC hardware via IPMI 2.0 (LAN+)",
        Attributes: map[string]schema.Attribute{
            "host": schema.StringAttribute{
                Required:    true,
                Description: "IPMI BMC IP address or hostname",
            },
            "username": schema.StringAttribute{
                Required:    true,
                Description: "IPMI username",
            },
            "password": schema.StringAttribute{
                Required:    true,
                Sensitive:   true,
                Description: "IPMI password",
            },
            "port": schema.Int64Attribute{
                Optional:    true,
                Description: "IPMI UDP port (default: 623)",
            },
            "interface": schema.StringAttribute{
                Optional:    true,
                Description: "IPMI interface: lanplus (default), lan, or open",
            },
        },
    }
}
```

***

## Resource: `ipmi_power`

This is the core resource — maps cleanly to Terraform's CRUD lifecycle:

| Terraform lifecycle | IPMI operation |
|---|---|
| `Create` | `chassis power on` (if desired state = on) |
| `Read` | `chassis power status` → update state |
| `Update` | `chassis power cycle/reset/off` |
| `Delete` | `chassis power off` (or no-op, configurable) |

```go
// internal/resources/power_resource.go
type powerResourceModel struct {
    Host         types.String `tfsdk:"host"`        // override provider host
    State        types.String `tfsdk:"state"`        // "on", "off", "cycle", "reset"
    CurrentState types.String `tfsdk:"current_state"` // computed
    LastUpdated  types.String `tfsdk:"last_updated"`  // computed
}

func (r *powerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
    resp.Schema = schema.Schema{
        Attributes: map[string]schema.Attribute{
            "state": schema.StringAttribute{
                Required:    true,
                Description: "Desired power state: on, off, cycle, reset, soft",
                Validators:  []validator.String{
                    stringvalidator.OneOf("on", "off", "cycle", "reset", "soft"),
                },
            },
            "current_state": schema.StringAttribute{Computed: true},
            "last_updated":  schema.StringAttribute{Computed: true},
        },
    }
}
```

**Example HCL usage:**

```hcl
provider "ipmi" {
  host     = "192.0.2.50"
  username = "root"
  password = "calvin"
}

resource "ipmi_power" "r210ii" {
  state = "on"
}

output "power_state" {
  value = ipmi_power.r210ii.current_state
}
```

***

## Resource: `ipmi_boot_device`

```go
// Desired boot device: pxe, disk, bios, cdrom, floppy
type bootDeviceResourceModel struct {
    Device     types.String `tfsdk:"device"`      // pxe, disk, bios, cdrom
    Persistent types.Bool   `tfsdk:"persistent"`   // one-time or persistent
}
```

Maps to `ipmitool chassis bootdev <device> [options=persistent]` — the underlying IPMI Set System Boot Options command (0x08). Particularly useful for PXE workflows alongside other provisioning tools.[^12]

***

## Resource: `ipmi_user`

Manages IPMI user slots (1–15). Implements Create (set username + password + privilege), Read (get user access), Update (change password/privilege), Delete (clear user).

```go
type userResourceModel struct {
    UserID    types.Int64  `tfsdk:"user_id"`    // 1-15
    Username  types.String `tfsdk:"username"`
    Password  types.String `tfsdk:"password"`   // sensitive
    Privilege types.String `tfsdk:"privilege"`  // user, operator, admin, oem
    Enabled   types.Bool   `tfsdk:"enabled"`
}
```

***

## Data Sources

### `ipmi_chassis_status`
Returns: power state, power overload, power interlock, main power fault, chassis intrusion, cooling fault, drive fault.

### `ipmi_sensors`
Returns a list of all SDR sensor readings (temperature, fan RPM, voltage, current). Useful for monitoring/alerting in Terraform outputs or for driving conditional logic.

### `ipmi_sel`
Returns the system event log (SEL) entries — most recent N events with timestamp, sensor, event direction, and description.

***

## IPMI Client Interface (for Testability)

Define an interface that both the real `bougou/go-ipmi` client and the mock implement. This is the key to unit testing without real hardware:[^13][^14]

```go
// internal/ipmi/client.go
type BMCClient interface {
    Connect() error
    Close() error
    GetChassisStatus() (*ChassisStatus, error)
    SetPowerState(state PowerState) error
    SetBootDevice(device BootDevice, persistent bool) error
    GetSensors() ([]Sensor, error)
    SetUserName(userID uint8, username string) error
    SetUserPassword(userID uint8, password string) error
    SetUserAccess(userID uint8, privilege Privilege, enabled bool) error
}

// internal/ipmi/client_mock.go
type MockBMCClient struct {
    PowerState    string
    ShouldError   bool
    ConnectCalled bool
}

func (m *MockBMCClient) GetChassisStatus() (*ChassisStatus, error) {
    if m.ShouldError { return nil, fmt.Errorf("mock error") }
    return &ChassisStatus{PowerOn: m.PowerState == "on"}, nil
}
// ... implement remaining methods
```

The provider's `Configure` method returns the client interface, allowing tests to inject the mock.[^13]

***

## Testing Strategy

Testing a hardware-dependent provider requires a layered approach. No single strategy covers everything — use all three layers together.

### Layer 1: Unit Tests (No Hardware, No Network)

Use the `MockBMCClient` interface to test all resource logic in pure Go. These run in milliseconds and cover:[^14]

- Correct IPMI commands are called for each Terraform operation
- Error handling and diagnostics propagation
- State mapping correctness (IPMI response → Terraform state)

```go
// internal/resources/power_resource_test.go
func TestPowerResourceCreate_On(t *testing.T) {
    mock := &ipmi.MockBMCClient{PowerState: "off"}
    r := &powerResource{client: mock}
    // call r.Create() with a plan of state="on"
    // assert mock.SetPowerStateCalled == true
    // assert mock.LastSetState == PowerStateOn
}
```

### Layer 2: Acceptance Tests Against VirtualBMC (CI-Safe)

**VirtualBMC** (`pip install virtualbmc`) wraps KVM/libvirt VMs to expose a real IPMI 2.0 endpoint, allowing full provider acceptance tests without physical hardware. This is used by OpenStack's CI pipeline for exactly this purpose.[^15][^16]

```bash
# Setup in CI or local dev
pip install virtualbmc
vbmc add test-vm --port 6230 --username admin --password password
vbmc start test-vm

# Verify
ipmitool -I lanplus -U admin -P password -H 127.0.0.1 -p 6230 power status
```

Set the acceptance test environment variables:

```bash
export TF_ACC=1
export IPMI_HOST=127.0.0.1
export IPMI_PORT=6230
export IPMI_USERNAME=admin
export IPMI_PASSWORD=password
```

HashiCorp's `terraform-plugin-testing` module drives `terraform apply`/`terraform destroy` cycles against these real IPMI endpoints:[^17]

```go
// internal/resources/power_resource_test.go (acceptance)
func TestAccPowerResource_basic(t *testing.T) {
    resource.Test(t, resource.TestCase{
        ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
        Steps: []resource.TestStep{
            {
                Config: `
                  resource "ipmi_power" "test" {
                    state = "on"
                  }
                `,
                Check: resource.ComposeTestCheckFunc(
                    resource.TestCheckResourceAttr("ipmi_power.test", "current_state", "on"),
                ),
            },
            {
                Config: `
                  resource "ipmi_power" "test" {
                    state = "off"
                  }
                `,
                Check: resource.TestCheckResourceAttr("ipmi_power.test", "current_state", "off"),
            },
        },
    })
}
```

### Layer 3: `ipmi_sim` — IPMI BMC Simulator (Optional, No VM Required)

`ipmi_sim` from the **OpenIPMI** project is a standalone IPMI LAN BMC simulator accessible over IPMI 1.5 and 2.0. It emulates chassis control commands without needing KVM. Metal-stack's mini-lab project evaluated it specifically for provider test coverage.[^18][^19]

QEMU also embeds an IPMI BMC simulator natively via the `-device ipmi-bmc-sim` flag, which can be connected to `ipmi_sim` for a fully software-defined test target.[^19]

### Layer 4: Terraform Mock Provider Tests (Module Consumer Testing)

For testing Terraform *modules that consume* the IPMI provider (not the provider itself), Terraform 1.7+ supports `mock_provider` in `.tftest.hcl` files. This is used by module authors who don't want to spin up VirtualBMC just to test module logic:[^20][^21]

```hcl
# test/power_on.tftest.hcl
mock_provider "ipmi" {
  mock_resource "ipmi_power" {
    defaults = {
      current_state = "on"
      last_updated  = "2026-01-01T00:00:00Z"
    }
  }
}

run "power_on_test" {
  command = plan
  assert {
    condition     = ipmi_power.r210ii.state == "on"
    error_message = "Expected power state on"
  }
}
```

### CI Configuration (GitHub Actions)

```yaml
# .github/workflows/test.yml
name: Tests
on: [push, pull_request]
jobs:
  unit:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - run: go test ./... -v -cover

  acceptance:
    runs-on: ubuntu-latest
    services:
      # VirtualBMC requires a libvirt domain; use ipmi_sim instead for pure CI
    steps:
      - uses: actions/checkout@v4
      - run: sudo apt-get install -y openipmi
      - run: |
          # Start ipmi_sim on port 9001
          ipmi_sim -c test/ipmi_sim.conf &
          export TF_ACC=1
          export IPMI_HOST=127.0.0.1
          export IPMI_PORT=9001
          go test ./... -run TestAcc -v -timeout 30m
```

***

## Mixed-Fleet Example: iDRAC6 + iDRAC7

This is the target end-state — managing all three servers from a single Terraform configuration:

```hcl
terraform {
  required_providers {
    ipmi = {
      source  = "yourorg/ipmi"
      version = "~> 0.1"
    }
    redfish = {
      source  = "dell/redfish"
      version = "~> 1.5"
    }
  }
}

# R210 II — iDRAC6 via IPMI
provider "ipmi" {
  host      = var.r210ii_idrac_ip
  username  = "root"
  password  = var.r210ii_password
  interface = "lanplus"
}

# iDRAC7 servers — Redfish
provider "redfish" {
  redfish_servers = {
    r620_1 = {
      user        = "root"
      password    = var.r620_1_password
      endpoint    = "https://${var.r620_1_idrac_ip}"
      ssl_insecure = true
    }
    r620_2 = {
      user        = "root"
      password    = var.r620_2_password
      endpoint    = "https://${var.r620_2_idrac_ip}"
      ssl_insecure = true
    }
  }
}

# R210 II power management
resource "ipmi_power" "r210ii" {
  state = "on"
}

resource "ipmi_boot_device" "r210ii_boot" {
  device     = "disk"
  persistent = true
}

# iDRAC7 power management via Redfish
resource "redfish_power" "r620_1" {
  for_each     = { r620_1 = var.r620_1_idrac_ip }
  redfish_server { ... }
  desired_power_action = "On"
}
```

***

## Publishing to Terraform Registry

Once the provider is stable, publishing to `registry.terraform.io` allows installation via standard `required_providers` blocks:[^11][^10]

1. Create a GitHub repo named `terraform-provider-ipmitool`
2. Add a `goreleaser.yml` with the HashiCorp-required build matrix (linux/darwin/windows × amd64/arm64)
3. Sign releases with GPG (`gpg --full-generate-key`)
4. Add GPG public key to Terraform Registry account
5. Tag a release → GoReleaser publishes binaries → Registry picks them up automatically

The provider will then be installable as:

```hcl
source = "yourorg/ipmi"
```

***

## Effort Estimate

| Phase | Scope | Estimate |
|---|---|---|
| Scaffold + provider config + client interface | main.go, provider.go, client.go | 2–3 hours |
| `ipmi_power` resource + unit tests | Core use case | 3–4 hours |
| `ipmi_boot_device` resource + tests | Boot control | 2–3 hours |
| `ipmi_chassis_status` data source | Read-only | 1–2 hours |
| VirtualBMC acceptance test setup | CI integration | 2–3 hours |
| `ipmi_user` resource | User management | 3–4 hours |
| `ipmi_sensors` data source | SDR reads | 2–3 hours |
| Docs + registry publish | README, examples | 2–3 hours |
| **Total v0.1 (power + boot + chassis)** | | **~1.5 days** |
| **Total v0.2 (users + sensors + full CI)** | | **~3 days total** |

***

## Key References

- `bougou/go-ipmi` — pure Go IPMI library: https://github.com/bougou/go-ipmi[^8]
- Terraform Plugin Framework docs: https://developer.hashicorp.com/terraform/plugin/framework[^11]
- Acceptance test framework: https://developer.hashicorp.com/terraform/plugin/framework/acctests[^17]
- VirtualBMC (OpenStack): https://docs.openstack.org/virtualbmc/latest/user/index.html[^16]
- `ipmi_sim` man page: https://www.mankier.com/1/ipmi_sim[^18]
- Dell iDRAC6 no Redfish confirmation: https://github.com/dell/iDRAC-Redfish-Scripting/issues/15[^5]
- Dell terraform-provider-redfish: https://github.com/dell/terraform-provider-redfish[^2]
- Terraform mock providers (1.7+): https://developer.hashicorp.com/terraform/language/tests[^21]

---

## References

1. [iDRAC: Redfish API with Dell integrated Remote Access Controller](https://www.dell.com/support/kbdoc/en-us/000178045/redfish-api-with-dell-integrated-remote-access-controller) - This article provides guidance on using the Redfish API with Dell integrated Remote Access Controlle...

2. [Terraform provider for Redfish REST APIs - GitHub](https://github.com/dell/terraform-provider-redfish) - The Terraform Provider can be used to manage server power cycles, IDRAC attributes, BIOS attributes,...

3. [bmc-toolbox/bmclib: Library to abstract Baseboard Management ...](https://github.com/bmc-toolbox/bmclib) - bmclib performs queries on BMCs using multiple drivers , these drivers are the various services expo...

4. [Dell iDRAC Restful Redfish API Agent - Page 2 - Checkmk Forum](https://forum.checkmk.com/t/dell-idrac-restful-redfish-api-agent/29458?page=2) - This iDRAC 7 don't support all the Redfish features like the newer 8 and 9 versions. What tool do yo...

5. [404 response code for python api · Issue #15 · dell/iDRAC-Redfish-Scripting](https://github.com/dell/iDRAC-Redfish-Scripting/issues/15) - url = 'https://%s/redfish/v1/Systems/System.Embedded.1/Actions/ComputerSystem.Reset' % idrac_ip payl...

6. [Terraform Provider for RedFish - Open Source at Dell](https://dell.github.io/terraform-docs/docs/server/platforms/redfish/readme/) - The Terraform Provider can be used to manage server power cycles, IDRAC attributes, BIOS attributes,...

7. [Package: golang-github-bougou-go-ipmi-dev (0.7.2-2)](https://packages.debian.org/sid/all/golang-github-bougou-go-ipmi-dev) - Pure Go IPMI client library

8. [bougou/go-ipmi: IPMI client library in pure Go - GitHub](https://github.com/bougou/go-ipmi) - For example, ipmitool sdr list involves a loop of GetSDR IPMI commands. This library also implements...

9. [ipmitool](https://pkg.go.dev/github.com/squarefactory/ipmitool)

10. [Custom Framework Providers | Terraform - HashiCorp Developer](https://developer.hashicorp.com/terraform/tutorials/providers-plugin-framework) - In these tutorials, learn how Terraform uses providers to interact with target APIs. Then, build a c...

11. [Plugin development | Terraform - HashiCorp Developer](https://developer.hashicorp.com/terraform/plugin) - Learn about plugin framework benefits and why we recommend using it to develop providers. Try these ...

12. [An open-source tool for controlling IPMI-enabled systems](https://github.com/ipmitool/ipmitool) - An open-source tool for controlling IPMI-enabled systems - ipmitool/ipmitool

13. [How can I inject a URL of a mock server to terraform's acceptance ...](https://stackoverflow.com/questions/68699294/how-can-i-inject-a-url-of-a-mock-server-to-terraforms-acceptance-tests) - Construct mock API client and pass it to test provider. Allow passing api client to provider constur...

14. [How to Implement Terraform Provider Testing - OneUptime](https://oneuptime.com/blog/post/2026-01-30-how-to-implement-terraform-provider-testing/view) - Learn how to write and run tests for Terraform providers using the acceptance testing framework and ...

15. [VirtualBMC](https://docs.openstack.org/developer/tripleo-docs/environments/virtualbmc.html)

16. [How to use VirtualBMC - OpenStack Documentation](https://docs.openstack.org/virtualbmc/latest/user/index.html)

17. [Acceptance tests | Terraform - HashiCorp Developer](https://developer.hashicorp.com/terraform/plugin/framework/acctests) - Learn how to write acceptance tests for providers built on the framework. Acceptance tests help ensu...

18. [ipmi_sim: IPMI LAN BMC Simulator | Man Page - ManKier](https://www.mankier.com/1/ipmi_sim) - The ipmi_sim daemon emulates an IPMI BMC simulator that may be accessed using the IPMI 1.5 or 2.0 LA...

19. [Try IPMI simulator (virtual BMC) again · Issue #116 - GitHub](https://github.com/metal-stack/mini-lab/issues/116) - This would give us much higher test coverage as also the ipmi_sim from OpenIPMI seems to be pretty m...

20. [The Built-in Terraform Module Testing Framework, No Go Required](https://dev.to/recca0120/terraform-test-the-built-in-terraform-module-testing-framework-no-go-required-18ap) - Terraform 1.6 ships a built-in test framework. Write tests in .tftest.hcl, run with terraform test. ...

21. [Tests - Configuration Language | Terraform](https://developer.hashicorp.com/terraform/language/tests) - Write structured test code for validating your configuration.

