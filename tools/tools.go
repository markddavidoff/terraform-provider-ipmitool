//go:build tools

package tools

import (
	_ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
	_ "golang.org/x/vuln/cmd/govulncheck"
	_ "honnef.co/go/tools/cmd/staticcheck"
)
