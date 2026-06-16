package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccUserResource_basic exercises CRUD against a real BMC. Most BMCs
// accept remote user CRUD over LAN, but Dell 11G bare BMCs return
// "Invalid data field in request" for Set User Name regardless of slot,
// privilege, or disable-first sequencing — likely a firmware restriction
// that requires user mgmt via Lifecycle Controller / RACADM instead.
//
// Gate the test on IPMI_USER_TEST=1 so it only runs against BMCs known
// to support it. The provider itself works correctly; this is purely a
// known limitation of the R210 II BMC we develop against.
func TestAccUserResource_basic(t *testing.T) {
	skipUnlessAcc(t)
	if os.Getenv("IPMI_USER_TEST") == "" {
		t.Skip("IPMI_USER_TEST not set; skipping — Dell 11G BMC rejects remote Set User Name")
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerHCL + `
resource "ipmi_user" "tf_test" {
  user_id       = 4
  name          = "tftest"
  user_password = "verylongtestpass1!"
  privilege     = "user"
  enabled       = true
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("ipmi_user.tf_test", "user_id", "4"),
					resource.TestCheckResourceAttr("ipmi_user.tf_test", "name", "tftest"),
					resource.TestCheckResourceAttr("ipmi_user.tf_test", "privilege", "user"),
					resource.TestCheckResourceAttr("ipmi_user.tf_test", "enabled", "true"),
					resource.TestCheckResourceAttr("ipmi_user.tf_test", "channel", "1"),
					resource.TestCheckResourceAttrSet("ipmi_user.tf_test", "id"),
				),
			},
			// Drift check.
			{
				Config: providerHCL + `
resource "ipmi_user" "tf_test" {
  user_id       = 4
  name          = "tftest"
  user_password = "verylongtestpass1!"
  privilege     = "user"
  enabled       = true
}
`,
				PlanOnly: true,
			},
		},
	})
}

// TestAccUserResource_selfDisableBlocked verifies the lockout guard:
// disabling the connection user without force_lockout_risk should error.
//
// Uses ExpectError to assert the diagnostic fires; nothing is actually
// applied so the BMC's connection user stays untouched.
func TestAccUserResource_selfDisableBlocked(t *testing.T) {
	skipUnlessAcc(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerHCL + `
resource "ipmi_user" "self" {
  user_id       = 2
  name          = var.ipmi_user
  user_password = "ignored"
  privilege     = "administrator"
  enabled       = false
}
`,
				ExpectError: regexpSelfDisable,
				PlanOnly:    true,
			},
		},
	})
}
