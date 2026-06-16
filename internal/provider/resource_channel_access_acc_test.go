package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccChannelAccessResource_channel1DisableBlocked confirms the
// self-lockout guard fires when a plan would disable LAN access on
// channel 1 (the channel Terraform connects through).
//
// PlanOnly + ExpectError means we never apply — safe to run against
// the real R210 II.
func TestAccChannelAccessResource_channel1DisableBlocked(t *testing.T) {
	skipUnlessAcc(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerHCL + `
resource "ipmi_channel_access" "lockout" {
  channel     = 1
  access_mode = "disabled"
}
`,
				ExpectError: regexp.MustCompile(`channel 1 access_mode.*disabled.*lock`),
				PlanOnly:    true,
			},
		},
	})
}

// (The "forced → warning, no error" path is covered by
// TestEnforceLockoutGuards in lockout_guard_test.go — it doesn't need a
// hardware-touching acceptance test.)
