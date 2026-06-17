package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccBootDeviceResource_oneShot exercises the persistent=false path.
// One-shot overrides do NOT report drift on subsequent plans because BIOS
// auto-clears them (POC 1 finding) — the second step's PlanOnly check
// proves that.
func TestAccBootDeviceResource_oneShot(t *testing.T) {
	skipUnlessAcc(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerHCL + `
resource "ipmi_boot_device" "test" {
  device     = "none"
  persistent = false
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("ipmi_boot_device.test", "device", "none"),
					resource.TestCheckResourceAttr("ipmi_boot_device.test", "persistent", "false"),
					resource.TestCheckResourceAttrSet("ipmi_boot_device.test", "id"),
				),
			},
			// Drift check: same config should plan as no-op.
			{
				Config: providerHCL + `
resource "ipmi_boot_device" "test" {
  device     = "none"
  persistent = false
}
`,
				PlanOnly: true,
			},
		},
	})
}
