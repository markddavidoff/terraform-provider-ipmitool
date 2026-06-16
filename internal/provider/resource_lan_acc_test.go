package provider

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccLanResource_channel1IPChangeBlocked verifies the lockout guard
// fires when channel 1's ip_address is explicitly set. Plan-only +
// ExpectError → safe against real hardware.
func TestAccLanResource_channel1IPChangeBlocked(t *testing.T) {
	skipUnlessAcc(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerHCL + `
resource "ipmi_lan" "lockout" {
  channel    = 1
  ip_address = "10.99.99.99"
}
`,
				ExpectError: regexp.MustCompile(`channel 1 ip_address change may break BMC connectivity`),
				PlanOnly:    true,
			},
		},
	})
}

// TestAccLanResource_channel1DHCPBlocked verifies the same guard fires
// when switching channel 1 to DHCP.
func TestAccLanResource_channel1DHCPBlocked(t *testing.T) {
	skipUnlessAcc(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerHCL + `
resource "ipmi_lan" "lockout" {
  channel   = 1
  ip_source = "dhcp"
}
`,
				ExpectError: regexp.MustCompile(`channel 1 ip_source.*dhcp.*break BMC connectivity`),
				PlanOnly:    true,
			},
		},
	})
}

// TestAccLanResource_readOnly creates a resource with the host's current
// settings reflected back, exercising the full Read path. Gated by
// IPMI_LAN_TEST because Create writes too — the test attempts no actual
// change but writes whatever values the user has set are still IPMI Set
// LAN Config Param calls.
func TestAccLanResource_readOnly(t *testing.T) {
	skipUnlessAcc(t)
	if os.Getenv("IPMI_LAN_TEST") == "" {
		t.Skip("IPMI_LAN_TEST not set; skipping — Create writes LAN params even with same values")
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerHCL + `
resource "ipmi_lan" "self" {
  channel = 1
  # No write fields — only reads.
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("ipmi_lan.self", "channel", "1"),
					resource.TestCheckResourceAttrSet("ipmi_lan.self", "ip_address"),
					resource.TestCheckResourceAttrSet("ipmi_lan.self", "mac_address"),
					resource.TestCheckResourceAttrSet("ipmi_lan.self", "id"),
				),
			},
		},
	})
}
