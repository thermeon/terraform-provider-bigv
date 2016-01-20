package main

import (
	"github.com/Thermeon/terraform-provider-bigv/bigv"
	"github.com/hashicorp/terraform/plugin"
)

func main() {
	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: bigv.Provider,
	})
}
