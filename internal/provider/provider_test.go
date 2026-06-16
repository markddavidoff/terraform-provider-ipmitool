package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories is the provider factory acceptance
// tests use. Every TestAcc* test references this so they all share the
// same in-process provider server.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"ipmi": providerserver.NewProtocol6WithError(New("test")()),
}

// skipUnlessAcc skips a test unless TF_ACC is set AND the required env
// vars for talking to a real BMC are present. Acceptance tests reach
// out to actual hardware and shouldn't run in `go test ./...` by default.
func skipUnlessAcc(t *testing.T) {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set; skipping acceptance test")
	}
	for _, k := range []string{"IPMI_HOST", "IPMI_USER", "IPMI_PASS"} {
		if os.Getenv(k) == "" {
			t.Skipf("%s not set; skipping acceptance test", k)
		}
	}
}

// providerHCL emits a provider block that pulls credentials from env via
// terraform's TF_VAR_<name> mechanism. The Makefile's testacc target
// sets TF_VAR_ipmi_* alongside TF_ACC so the test process sees both.
const providerHCL = `
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
`
