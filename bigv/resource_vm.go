package bigv

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"

	"github.com/hashicorp/terraform/helper/schema"
)

const passwordLength = 20

type bigvVm struct {
	Name   string `json:"name"`
	Cores  int    `json:"cores"`
	Memory int    `json:"memory"`
}

type bigvDisc struct {
	Label        string `json:"label"`
	StorageGrade string `json:"storage_grade"`
	Size         int    `json:"size"`
}

type bigvImage struct {
	Distribution string `json:"distribution"`
	RootPassword string `json:"root_password"`
	SshPublicKey string `json:"ssh_public_key"`
}

type bigvNetwork struct {
	Ipv4 string `json:"ipv4"`
}

type bigvServer struct {
	VirtualMachine bigvVm      `json:"virtual_machine"`
	Discs          []bigvDisc  `json:"discs"`
	Reimage        bigvImage   `json:"reimage"`
	Ips            bigvNetwork `json:"ips"`
}

func resourceBigvVM() *schema.Resource {
	return &schema.Resource{
		Create: resourceBigvVMCreate,
		Delete: resourceBigvVMDelete,
		Schema: map[string]*schema.Schema{
			"name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"os": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				Default:  "vivid",
				ForceNew: true,
			},
			"cores": &schema.Schema{
				Type:     schema.TypeInt,
				Optional: true,
				Default:  "1",
				ForceNew: true,
			},
			"memory": &schema.Schema{
				Type:     schema.TypeInt,
				Optional: true,
				Default:  "1024",
				ForceNew: true,
			},
			"disc_size": &schema.Schema{
				Type:     schema.TypeInt,
				Optional: true,
				Default:  "25600",
				ForceNew: true,
			},
		},
	}
}

func resourceBigvVMCreate(d *schema.ResourceData, meta interface{}) error {

	client := meta.(*Client)

	vm := bigvServer{
		VirtualMachine: bigvVm{
			Name:   d.Get("name").(string),
			Cores:  d.Get("cores").(int),
			Memory: d.Get("memory").(int),
		},
		Discs: []bigvDisc{{
			Label:        "root",
			StorageGrade: "sata",
			Size:         d.Get("disc_size").(int),
		}},
		Reimage: bigvImage{
			Distribution: d.Get("os").(string),
			RootPassword: randomPassword(),
		},
	}

	j, err := json.Marshal(vm)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "JSON: %s\n", j)

	_ = client
	_ = vm

	// TODO - Id from bytemark
	d.SetId(d.Get("name").(string))

	return nil
}

func resourceBigvVMDelete(d *schema.ResourceData, meta interface{}) error {

	return nil
}

const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*()-_=+,.?/:;{}[]`~"

func randomPassword() string {
	b := make([]byte, passwordLength)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

/* TODO:
 * Check distribution
 * Disc isn't flexible at all
 * Allocate ip automatically
 * Set id properly
 */
