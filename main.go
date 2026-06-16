// terraform-provider-ipmitool is a Terraform provider that orchestrates
// BMC hardware via IPMI 2.0 (LAN+) by wrapping the ipmitool CLI.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/markddavidoff/terraform-provider-ipmitool/internal/provider"
)

// version is overridden at release time via -ldflags.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers (e.g. delve)")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/markddavidoff/ipmitool",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
