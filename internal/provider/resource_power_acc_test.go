package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccPowerResource_basic verifies the full Create → Read → No-op
// drift cycle against real hardware. Gated by TF_ACC + IPMI_HOST/USER/PASS;
// run via `make testacc`.
//
// Sets state = "on" twice in a row and expects no drift on the second
// pass. The host should already be on for this test to be safe — we don't
// want to silently power-off-and-back-on the user's hardware.
func TestAccPowerResource_basic(t *testing.T) {
	skipUnlessAcc(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerHCL + `
resource "ipmi_power" "test" {
  state = "on"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("ipmi_power.test", "state", "on"),
					resource.TestCheckResourceAttr("ipmi_power.test", "current_state", "on"),
					resource.TestCheckResourceAttrSet("ipmi_power.test", "id"),
				),
			},
			// Second step with same config — should plan as no-op.
			{
				Config: providerHCL + `
resource "ipmi_power" "test" {
  state = "on"
}
`,
				PlanOnly: true,
			},
		},
	})
}
