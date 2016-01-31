package bigv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/hashicorp/terraform/helper/schema"
)

const passwordLength = 20
const waitForVM = 120

type bigvVm struct {
	Id           int    `json:"id,omitempty"`
	Name         string `json:"name,omitempty"`
	Cores        int    `json:"cores,omitempty"`
	Memory       int    `json:"memory,omitempty"`
	Hostname     string `json:"hostname,omitempty"`
	Distribution string `json:"last_imaged_with,omitempty"`
	Power        bool   `json:"power_on"`
	Reboot       bool   `json:"autoreboot_on"`
	Group        string `json:"group,omitempty"`
	GroupId      int    `json:"group_id,omitempty"`
	Zone         string `json:"zone_name,omitempty"`
}

type bigvDisc struct {
	Label        string `json:"label,omitempty"`
	StorageGrade string `json:"storage_grade,omitempty"`
	Size         int    `json:"size,omitempty"`
}

type bigvImage struct {
	Distribution string `json:"distribution,omitempty"`
	RootPassword string `json:"root_password,omitempty"`
	SshPublicKey string `json:"ssh_public_key,omitempty"`
}

type bigvIps struct {
	// Create attributes
	Ipv4 string `json:"ipv4,omitempty"`
	Ipv6 string `json:"ipv6,omitempty"`
}

type bigvNic struct {
	// Read Attributes
	Label string   `json:"label,omitempty"`
	Ips   []string `json:"ips,omitempty"`
	Mac   string   `json:"mac,omitempty"`
}

type bigvServer struct {
	bigvVm
	Discs []bigvDisc `json:"discs,omitempty"`
	Nics  []bigvNic  `json:"network_interface,omitempty"`
}

type bigvVMCreate struct {
	VirtualMachine bigvVm     `json:"virtual_machine"`
	Discs          []bigvDisc `json:"discs,omitempty"`
	Image          bigvImage  `json:"reimage,omitempty"`
	Ips            bigvIps    `json:"ips,omitempty"` // Just used for create
}

func resourceBigvVM() *schema.Resource {
	return &schema.Resource{
		Create: resourceBigvVMCreate,
		Read:   resourceBigvVMRead,
		Update: resourceBigvVMUpdate,
		Delete: resourceBigvVMDelete,
		Exists: resourceBigvVMExists,
		Schema: map[string]*schema.Schema{
			"name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"group": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Default:     "default",
				Description: "bigv group name for the VM. Defaults to default",
			},
			"group_id": &schema.Schema{
				Type:     schema.TypeInt,
				Computed: true,
			},
			"zone": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Default:     "york",
				Description: "bigv zone to put the VM in. Defaults to york",
			},
			"ipv4": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"ipv6": &schema.Schema{
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
				Type:         schema.TypeInt,
				Optional:     true,
				Computed:     true,
				ComputedWhen: []string{"memory"},
			},
			"memory": &schema.Schema{
				Type:         schema.TypeInt,
				Optional:     true,
				Computed:     true,
				ComputedWhen: []string{"cores"},
			},
			"disc_size": &schema.Schema{
				Type:     schema.TypeInt,
				Optional: true,
				Default:  "25600",
				ForceNew: true,
			},
			"root_password": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},
			"power_on": &schema.Schema{
				Type:        schema.TypeBool,
				Default:     true,
				Optional:    true,
				Description: "Whether or not the VM should be powered. Note that with reboot true, power_on false is just a reboot",
			},
			"reboot": &schema.Schema{
				Type:        schema.TypeBool,
				Default:     true,
				Optional:    true,
				Description: "Whether or not to reboot the VM when the power_on is turned off",
			},
			"ssh_public_key": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				Description: "One or more ssh public keys to put on the machine. Will only work if os is not core",
			},
		},
	}
}

func resourceBigvVMCreate(d *schema.ResourceData, meta interface{}) error {

	l := log.New(os.Stderr, "", 0)

	bigvClient := meta.(*client)

	vm := bigvVMCreate{
		VirtualMachine: bigvVm{
			Name:   d.Get("name").(string),
			Cores:  d.Get("cores").(int),
			Memory: d.Get("memory").(int),
			Power:  d.Get("power_on").(bool),
			Reboot: d.Get("reboot").(bool),
			Group:  d.Get("group").(string),
			Zone:   d.Get("zone").(string),
		},
		Discs: []bigvDisc{{
			Label:        "root",
			StorageGrade: "sata",
			Size:         d.Get("disc_size").(int),
		}},
		Image: bigvImage{
			Distribution: d.Get("os").(string),
			RootPassword: randomPassword(),
			SshPublicKey: d.Get("ssh_public_key").(string),
		},
		Ips: bigvIps{
			Ipv4: d.Get("ipv4").(string),
			Ipv6: d.Get("ipv6").(string),
		},
	}

	if err := vm.VirtualMachine.computeCoresToMemory(); err != nil {
		return err
	}

	if vm.Image.SshPublicKey != "" && vm.Image.Distribution == "none" {
		return errors.New("Cannot deploy ssh public keys with an os of 'none'. Please use a provisioner instead")
	}

	body, err := json.Marshal(vm)
	if err != nil {
		return err
	}

	// VM create uses a bigger path
	url := fmt.Sprintf("%s/accounts/%s/groups/%s/vm_create",
		bigvUri,
		bigvClient.account,
		vm.VirtualMachine.Group, // this will be group name
	)

	l.Printf("Requesting VM create: %s", url)
	l.Printf("VM profile: %s", body)

	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))

	if resp, err := bigvClient.do(req); err != nil {
		return err
	} else {

		// Always close the body when done
		defer resp.Body.Close()

		l.Printf("HTTP response Status: %s", resp.Status)

		if resp.StatusCode != http.StatusAccepted {
			body, _ := ioutil.ReadAll(resp.Body)
			return fmt.Errorf("Create VM status %d from bigv: %s", resp.StatusCode, body)
		}

		for k, v := range resp.Header {
			l.Printf("%s: %s", k, v)
		}

		if err := waitForMachine(d, bigvClient); err != nil {
			return err
		}

		l.Printf("Created BigV VM, Id: %s", d.Id())

		return nil
	}

}

func waitForMachine(d *schema.ResourceData, bigvClient *client) error {
	l := log.New(os.Stderr, "", 0)

	url := fmt.Sprintf("%s/virtual_machines/%s?view=overview",
		bigvUri,
		d.Get("name"),
	)

	l.Printf("VM Health Check: %s", url)
	req, _ := http.NewRequest("GET", url, nil)

	for i := 0; i < waitForVM; i++ {
		resp, err := bigvClient.do(req)
		if err != nil {
			return fmt.Errorf("Error checking on VM health: %s", err)
		}

		// Always close the body when done
		defer resp.Body.Close()

		body, _ := ioutil.ReadAll(resp.Body)

		l.Printf("HTTP response Status: %s", resp.Status)
		l.Printf("HTTP response Body: %s", body)

		if i == 0 {
			// Use the first response to populate the body of our resource, in case we never do any better
			resourceFromJson(d, body)
		}

		if resp.StatusCode == http.StatusOK {
			// The VM is up
			l.Println("VM is Up and OK")
			return resourceFromJson(d, body)
		}

		if resp.StatusCode != http.StatusAccepted {
			return fmt.Errorf("VM healthcheck Bad HTTP status from bigv: %d", resp.StatusCode)
		}

		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("VM healthcheck didn't return in %d seconds", waitForVM)
}

func resourceBigvVMUpdate(d *schema.ResourceData, meta interface{}) error {
	l := log.New(os.Stderr, "", 0)

	bigvClient := meta.(*client)

	vm := bigvVm{}

	if d.HasChange("power_on") {
		vm.Power = d.Get("power_on").(bool)
	}

	if d.HasChange("power_on") {
		vm.Reboot = d.Get("reboot").(bool)
	}

	if d.HasChange("cores") || d.HasChange("memory") {
		// Specifiy both cores and memory together always, so we can validate them.
		vm.Cores = d.Get("cores").(int)
		vm.Memory = d.Get("memory").(int)

		// Whenever we change either of these reboot the server
		// That's because even though decreasing ram doesn't require a reboot,
		// it looks like it often goes wrong and you get less ram than you should.
		// e.g. lowering to 1GB nearly always gives you 750MB
		if !d.HasChange("power_on") {
			vm.Power = false
			// Always need Reboot on, otherwise it'll stay down
			vm.Reboot = true
		}
	}

	if err := vm.computeCoresToMemory(); err != nil {
		return err
	}

	body, err := json.Marshal(vm)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/accounts/%s/groups/%s/virtual_machines/%s",
		bigvUri,
		bigvClient.account,
		d.Get("group"),
		d.Id(),
	)

	l.Printf("Requesting VM update: %s", url)
	l.Printf("VM profile: %s", body)

	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(body))
	if err != nil {
		l.Printf("Error creating request for Update: %s", err)
		return err
	}

	if resp, err := bigvClient.do(req); err != nil {
		return err
	} else {

		// Always close the body when done
		defer resp.Body.Close()

		l.Printf("HTTP response Status: %s", resp.Status)

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("Update VM bad status from bigv: %d", resp.StatusCode)
		}

		if body, err := ioutil.ReadAll(resp.Body); err != nil {
			return err
		} else {
			if err := resourceFromJson(d, body); err != nil {
				return err
			}
		}

		l.Printf("Updated BigV VM, Id: %s", d.Id())

		return nil
	}
	return nil
}

func resourceBigvVMRead(d *schema.ResourceData, meta interface{}) error {
	l := log.New(os.Stderr, "", 0)

	bigvClient := meta.(*client)

	url := fmt.Sprintf("%s/virtual_machines/%s?view=overview",
		bigvUri,
		d.Get("name"),
	)

	l.Printf("VM Read: %s", url)

	req, _ := http.NewRequest("GET", url, nil)

	resp, err := bigvClient.do(req)
	if err != nil {
		return err
	}

	// Always close the body when done
	defer resp.Body.Close()

	l.Printf("HTTP response Status: %s", resp.Status)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Read VM Bad HTTP status from bigv: %d", resp.StatusCode)
	}

	body, ioErr := ioutil.ReadAll(resp.Body)
	if ioErr != nil {
		return ioErr
	}

	return resourceFromJson(d, body)
}

func resourceBigvVMDelete(d *schema.ResourceData, meta interface{}) error {
	l := log.New(os.Stderr, "", 0)

	bigvClient := meta.(*client)

	url := fmt.Sprintf("%s/accounts/%s/groups/%s/virtual_machines/%s?purge=true",
		bigvUri,
		bigvClient.account,
		d.Get("group"),
		d.Id(),
	)
	l.Printf("Deleting VM at %s", url)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	if resp, err := bigvClient.do(req); err != nil {
		return err
	} else {
		// Always close the body when done
		defer resp.Body.Close()

		l.Printf("Delete %s HTTP response Status: %s", d.Id(), resp.Status)
		if resp.StatusCode != http.StatusNoContent {
			return fmt.Errorf("Delete VM %s Bad HTTP status from bigv: %d", d.Id(), resp.StatusCode)
		}
	}

	return nil
}

func resourceBigvVMExists(d *schema.ResourceData, meta interface{}) (bool, error) {
	l := log.New(os.Stderr, "", 0)

	bigvClient := meta.(*client)

	url := fmt.Sprintf("%s/virtual_machines/%s",
		bigvUri,
		d.Id(),
	)

	l.Printf("Checking VM existance at %s", url)

	req, _ := http.NewRequest("GET", url, nil)
	resp, err := bigvClient.do(req)
	if err != nil {
		return false, err
	}

	l.Printf("Exists %s HTTP response Status: %s", d.Id(), resp.Status)
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted {
		return true, nil
	} else if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}

	return false, fmt.Errorf("Unexpected HTTP status from VM exists check: %d", resp.StatusCode)
}

func resourceFromJson(d *schema.ResourceData, vmJson []byte) error {
	l := log.New(os.Stderr, "", 0)

	l.Printf("VM definition: %s", vmJson)

	vm := &bigvServer{}

	if err := json.Unmarshal(vmJson, vm); err != nil {
		return err
	}

	d.SetId(strconv.Itoa(vm.Id))
	d.Set("name", vm.Name)
	d.Set("cores", vm.Cores)
	d.Set("memory", vm.Memory)
	d.Set("power_on", vm.Power)
	d.Set("reboot", vm.Reboot)
	d.Set("group_id", vm.GroupId)
	d.Set("zone", vm.Zone)

	// If we don't get discs back, this was probably an update request
	if len(vm.Discs) == 1 {
		d.Set("disk_size", vm.Discs[0].Size)
	}

	// Distribution is empty in create response, leave it with what we sent in
	if vm.Distribution != "" {
		d.Set("os", vm.Distribution)
	}

	// Not finding the ips is fine, because they're not sent back in the create request
	if len(vm.Nics) > 0 {
		// This is fairly^Wvery^Wacceptably hacky
		d.Set("ipv4", vm.Nics[0].Ips[0])
		d.Set("ipv6", vm.Nics[1].Ips[1])
	}

	return nil
}

/* computeCoresToMemory
bigv charges per 1GiB memory, and you automatically get 1 more core per 4GiB.
See: http://www.bigv.io/prices
*/
func (v *bigvVm) computeCoresToMemory() error {

	switch {
	case v.Cores == 0 && v.Memory == 0:
		// Both unset, so just supply defaults
		v.Cores = 1
		v.Memory = 1024
	case v.Cores == 0:
		// Just the cores calculated
		v.Cores = int(math.Ceil(float64(v.Memory/4096) + float64(0.01)))
	case v.Memory == 0:
		// Just the memory calculated
		v.Memory = int(math.Max(1024, float64((v.Cores-1)*4096)))
	default:
		// Both set, so validate them
		expectedCores := int(math.Ceil(float64(v.Memory/4096) + float64(0.01)))
		if expectedCores != v.Cores {
			return fmt.Errorf("Memory and cores mismatch!\nExpected %d cores for your %dGiB memory, but you have %d.\nSpecify 1 cores per 4GiB memory.\nSee: http://www.bigv.io/prices", expectedCores, v.Memory/1024, v.Cores)
		}
	}

	return nil
}

const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789@%&-_=+:~"

func randomPassword() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	b := make([]byte, passwordLength)
	for i := range b {
		b[i] = letters[r.Intn(len(letters))]
	}
	return string(b)
}
