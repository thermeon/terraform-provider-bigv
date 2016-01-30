package bigv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"sync"
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
				Type:     schema.TypeInt,
				Optional: true,
				Default:  "1",
			},
			"memory": &schema.Schema{
				Type:     schema.TypeInt,
				Optional: true,
				Default:  "1024",
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
				Type:     schema.TypeBool,
				Computed: true,
			},
			"reboot": &schema.Schema{
				Type:     schema.TypeBool,
				Computed: true,
			},
		},
	}
}

var createPipeline sync.Mutex

func resourceBigvVMCreate(d *schema.ResourceData, meta interface{}) error {

	l := log.New(os.Stderr, "", 0)

	// TODO - Early 2016, and we hope to remove this soonish
	// bigV deadlocks if you hit it with concurrent creates.
	// That might be an ip allocation issue, and specifying both ips might
	// fix it, but that's untested. For now waiting for them to confirm we
	// can lift this restriction.
	createPipeline.Lock()
	defer createPipeline.Unlock()

	bigvClient := meta.(*client)

	vm := bigvVMCreate{
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
		Image: bigvImage{
			Distribution: d.Get("os").(string),
			RootPassword: randomPassword(),
		},
		Ips: bigvIps{
			Ipv4: d.Get("ipv4").(string),
			Ipv6: d.Get("ipv6").(string),
		},
	}

	if err := vm.VirtualMachine.validateCoresToMemory(); err != nil {
		return err
	}

	body, err := json.Marshal(vm)
	if err != nil {
		return err
	}

	// VM create uses a bigger path
	url := fmt.Sprintf("%s/vm_create", bigvClient.fullUri())

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

	if d.HasChange("cores") || d.HasChange("memory") {
		// Specifiy both cores and memory together always, so we can validate them.
		vm.Cores = d.Get("cores").(int)
		vm.Memory = d.Get("memory").(int)

		// Whenever we change either of these reboot the server
		// That's because even though decreasing ram doesn't require a reboot,
		// it looks like it often goes wrong and you get less ram than you should.
		// e.g. lowering to 1GB nearly always gives you 750MB
		vm.Power = false
		// Always need Reboot on, otherwise it'll stay down
		vm.Reboot = true
	}

	if err := vm.validateCoresToMemory(); err != nil {
		return err
	}

	body, err := json.Marshal(vm)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/virtual_machines/%s",
		bigvClient.fullUri(),
		d.Id(),
	)

	l.Printf("Requesting VM update: %s", url)
	l.Printf("VM profile: %s", body)

	req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(body))

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

	bigvClient := meta.(*client)

	url := fmt.Sprintf("%s/virtual_machines/%s?purge=true",
		bigvClient.fullUri(),
		d.Id(),
	)
	req, _ := http.NewRequest("DELETE", url, nil)

	if resp, err := bigvClient.do(req); err != nil {
		return err
	} else {
		// Always close the body when done
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			return fmt.Errorf("Delete VM %s Bad HTTP status from bigv: %d", d.Id(), resp.StatusCode)
		}
	}

	return nil
}

func resourceBigvVMExists(d *schema.ResourceData, meta interface{}) (bool, error) {
	bigvClient := meta.(*client)

	url := fmt.Sprintf("%s/virtual_machines/%s?view=simple",
		bigvClient.fullUri(),
		d.Id(),
	)

	req, _ := http.NewRequest("GET", url, nil)
	resp, err := bigvClient.do(req)
	if err != nil {
		return false, err
	}

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

/* validateCoresToMemory
bigv charges per 1GiB memory, and you automatically get 1 more core per 4GiB.
That means we can't predict how much memory or cpu you should get, and we force you to be specific.
We could just always size servers by memory, and compute the RAM, but this is more explicit
See: http://www.bigv.io/prices
*/
func (v *bigvVm) validateCoresToMemory() error {
	// 1 core per 4GiB
	cores := int(math.Ceil(float64(v.Memory/4096) + float64(0.01)))

	if cores != v.Cores {
		return fmt.Errorf("Memory and cores mismatch!\nExpected %d cores for your %dGiB memory, but you have %d.\nSpecify 1 cores per 4GiB memory.\nSee: http://www.bigv.io/prices", cores, v.Memory/1024, v.Cores)
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

/* TODO:
 * Check distribution
 * Disc isn't flexible at all
 */
