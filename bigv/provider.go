package bigv

import (
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/terraform"
)

func Provider() terraform.ResourceProvider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"account": &schema.Schema{
				Type:        schema.TypeString,
				Required:    true,
				DefaultFunc: schema.EnvDefaultFunc("BIGV_ACCOUNT", nil),
				Description: "The bigv account name",
			},
			"user": &schema.Schema{
				Type:        schema.TypeString,
				Required:    true,
				DefaultFunc: schema.EnvDefaultFunc("BIGV_USER", nil),
				Description: "The bigv user name",
			},
			"password": &schema.Schema{
				Type:        schema.TypeString,
				Required:    true,
				DefaultFunc: schema.EnvDefaultFunc("BGIV_PASSWORD", nil),
				Description: "The bigv password",
			},
		},
		ResourcesMap: map[string]*schema.Resource{
			"bigv_vm": resourceBigvVM(),
		},
		ConfigureFunc: providerConfigure,
	}
}

func providerConfigure(d *schema.ResourceData) (bigvClient interface{}, err error) {

	bigvClient = &client{
		account:  d.Get("account").(string),
		user:     d.Get("user").(string),
		password: d.Get("password").(string),
	}

	return
}
