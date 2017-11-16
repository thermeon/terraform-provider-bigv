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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/terraform/helper/schema"
	"golang.org/x/crypto/ssh"
)

const (
	passwordLength     = 48
	waitForVM          = 1200
	vmCheckInterval    = 5
	waitForProvisioned = 1 + iota
	waitForPowered     = 1 + iota
)

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
	Distribution    string `json:"distribution,omitempty"`
	RootPassword    string `json:"root_password,omitempty"`
	SshPublicKey    string `json:"ssh_public_key,omitempty"`
	FirstBootScript string `json:"firstboot_script,omitempty"`
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
	Nics  []bigvNic  `json:"network_interfaces,omitempty"`
}

type bigvVMCreate struct {
	VirtualMachine bigvVm     `json:"virtual_machine"`
	Discs          []bigvDisc `json:"discs,omitempty"`
	Image          bigvImage  `json:"reimage,omitempty"`
	Ips            *bigvIps   `json:"ips,omitempty"` // Just used for create
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
				Optional: true,
				Computed: true,
			},
			"ipv6": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
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
			"firstboot_script": &schema.Schema{
				Type:        schema.TypeString,
				Optional:    true,
				Description: "A script to be executed on first boot arbitrarily",
			},
		},
	}
}

var createPipeline sync.Mutex

func resourceBigvVMCreate(d *schema.ResourceData, meta interface{}) error {
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
			Distribution:    d.Get("os").(string),
			RootPassword:    randomPassword(),
			SshPublicKey:    d.Get("ssh_public_key").(string),
			FirstBootScript: d.Get("firstboot_script").(string),
		},
	}

	// If no ipv* is set then let bigv allocate it itself
	// The json for ip must be nil
	if ip := d.Get("ipv4"); ip != nil && ip.(string) != "" {
		vm.Ips = &bigvIps{
			Ipv4: ip.(string),
		}
	}

	if ip := d.Get("ipv6"); ip != nil && ip.(string) != "" {
		if vm.Ips == nil {
			vm.Ips = &bigvIps{}
		}
		vm.Ips.Ipv6 = ip.(string)
	}

	// Make sure the root password gets stored in d
	d.Set("root_password", vm.Image.RootPassword)

	// Connection information
	connInfo := map[string]string{
		"type":     "ssh",
		"password": vm.Image.RootPassword,
	}
	if vm.Ips != nil {
		connInfo["host"] = vm.Ips.Ipv4
	}
	d.SetConnInfo(connInfo)

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

	log.Printf("[DEBUG] Requesting VM create: %s", url)
	log.Printf("[DEBUG] VM profile: %s", body)

	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))

	// TODO - Early 2016, and we hope to remove this soonish
	// bigV deadlocks if you hit it with concurrent creates.
	// That might be an ip allocation issue, and specifying both ips might
	// fix it, but that's untested. For now waiting for them to confirm we
	// can lift this restriction.
	createPipeline.Lock()
	resp, err := bigvClient.do(req)
	createPipeline.Unlock()
	if err != nil {
		return err
	}

	// Always close the body when done
	defer resp.Body.Close()

	log.Printf("[DEBUG] HTTP response Status: %s", resp.Status)

	if resp.StatusCode != http.StatusAccepted {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("Create VM status %d from bigv: %s", resp.StatusCode, body)
	}

	d.Partial(true)
	for _, i := range []string{"name", "group_id", "group", "zone", "cores", "memory", "ipv4", "ipv6", "root_password"} {
		d.SetPartial(i)
	}

	for k, v := range resp.Header {
		log.Printf("[DEBUG] %s: %s", k, v)
	}

	// wait for state also sets up the resource from the read state we get back
	if err := waitForBigvState(d, bigvClient, waitForProvisioned); err != nil {
		return err
	}

	log.Printf("[DEBUG] Created BigV VM, Id: %s", d.Id())

	// If we expect it to be turned on, wait for it to powered
	if vm.VirtualMachine.Power == true {
		if err := waitForBigvState(d, bigvClient, waitForPowered); err != nil {
			return err
		}

		// This assumes all distributions will listen on public ssh
		if vm.Image.Distribution != "none" {
			if err := waitForVmSsh(d); err != nil {
				return err
			}
		}
	}

	d.Partial(false)

	return nil

}

// waitForBigvState
// Obviously wait for a state
// Also sets up the resource from the state read
func waitForBigvState(d *schema.ResourceData, bigvClient *client, waitFor int) error {
	url := fmt.Sprintf("%s/virtual_machines/%s?view=overview",
		bigvUri,
		d.Get("name"),
	)

	log.Printf("[DEBUG] VM Health Check: %s", url)
	req, _ := http.NewRequest("GET", url, nil)

	var body []byte
	for {
		select {
		case <-time.After(waitForVM * time.Second):
			return fmt.Errorf("VM state didn't happen in %d seconds", waitForVM)
		case <-time.Tick(vmCheckInterval * time.Second):
			resp, err := bigvClient.do(req)
			if err != nil {
				return fmt.Errorf("Error checking on VM health: %s", err)
			}

			// Always close the body when done
			defer resp.Body.Close()

			body, _ = ioutil.ReadAll(resp.Body)

			log.Printf("[DEBUG] HTTP response Status: %s", resp.Status)
			// No matter what, update everything comes from the state
			if err := resourceFromJson(d, body); err != nil {
				return err
			}

			if resp.StatusCode == http.StatusOK {
				if waitFor == waitForProvisioned {
					log.Println("[DEBUG] VM is Up and HTTP OK")
					return nil
				}

				log.Println("[DEBUG] VM power:", d.Get("power_on").(bool))
				switch {
				case waitFor == waitForPowered && d.Get("power_on").(bool):
					log.Println("[DEBUG] VM is powered")
					return nil
				}
			}

			if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
				return fmt.Errorf("VM healthcheck Bad HTTP status from bigv: %d", resp.StatusCode)
			}
		}
	}

	return fmt.Errorf("VM state didn't happen in %d seconds", waitForVM)
}

// Simply waits for ssh to come up
func waitForVmSsh(d *schema.ResourceData) error {
	log.Printf("[DEBUG] Waiting for VM ssh: %s", d.Get("name"))

	config := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.Password(d.Get("root_password").(string)),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	for {
		select {
		case <-time.After(waitForVM * time.Second):
			return fmt.Errorf("VM ssh wasn't up in %d seconds", waitForVM)
		case <-time.Tick(vmCheckInterval * time.Second):
			conn, err := ssh.Dial("tcp", fmt.Sprintf("%s:22", d.Get("ipv4")), config)
			if err != nil {
				if strings.Contains(err.Error(), "connection refused") {
					log.Println("[DEBUG] SSH isn't up yet")
					continue
				} else {
					log.Printf("[DEBUG] SSH Error, ignored: %s", err.Error())
					continue
				}
			}
			conn.Close()
			log.Println("[DEBUG] SSH alive and kicking")
			return nil
		}
	}

	return errors.New("Ssh wait should never get here")
}

func resourceBigvVMUpdate(d *schema.ResourceData, meta interface{}) error {
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

	log.Printf("[DEBUG] Requesting VM update: %s", url)
	log.Printf("[DEBUG] VM profile: %s", body)

	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(body))
	if err != nil {
		log.Printf("[DEBUG] Error creating request for Update: %s", err)
		return err
	}

	if resp, err := bigvClient.do(req); err != nil {
		return err
	} else {

		// Always close the body when done
		defer resp.Body.Close()

		log.Printf("[DEBUG] HTTP response Status: %s", resp.Status)

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

		log.Printf("[DEBUG] Updated BigV VM, Id: %s", d.Id())

		return nil
	}
	return nil
}

func resourceBigvVMRead(d *schema.ResourceData, meta interface{}) error {
	bigvClient := meta.(*client)

	url := fmt.Sprintf("%s/virtual_machines/%s?view=overview",
		bigvUri,
		d.Get("name"),
	)

	log.Printf("[DEBUG] VM Read: %s", url)

	req, _ := http.NewRequest("GET", url, nil)

	resp, err := bigvClient.do(req)
	if err != nil {
		return err
	}

	// Always close the body when done
	defer resp.Body.Close()

	log.Printf("[DEBUG] HTTP response Status: %s", resp.Status)

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

	url := fmt.Sprintf("%s/accounts/%s/groups/%s/virtual_machines/%s?purge=true",
		bigvUri,
		bigvClient.account,
		d.Get("group"),
		d.Id(),
	)
	log.Printf("[DEBUG] Deleting VM at %s", url)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	if resp, err := bigvClient.do(req); err != nil {
		return err
	} else {
		// Always close the body when done
		defer resp.Body.Close()

		log.Printf("[DEBUG] Delete %s HTTP response Status: %s", d.Id(), resp.Status)
		if resp.StatusCode != http.StatusNoContent {
			return fmt.Errorf("Delete VM %s Bad HTTP status from bigv: %d", d.Id(), resp.StatusCode)
		}
	}

	return nil
}

func resourceBigvVMExists(d *schema.ResourceData, meta interface{}) (bool, error) {
	bigvClient := meta.(*client)

	url := fmt.Sprintf("%s/virtual_machines/%s",
		bigvUri,
		d.Id(),
	)

	log.Printf("[DEBUG] Checking VM existance at %s", url)

	req, _ := http.NewRequest("GET", url, nil)
	resp, err := bigvClient.do(req)
	if err != nil {
		return false, err
	}

	log.Printf("[DEBUG] Exists %s HTTP response Status: %s", d.Id(), resp.Status)
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted {
		return true, nil
	} else if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}

	return false, fmt.Errorf("Unexpected HTTP status from VM exists check: %d", resp.StatusCode)
}

func resourceFromJson(d *schema.ResourceData, vmJson []byte) error {
	log.Printf("[DEBUG] VM definition: %s", vmJson)

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
		d.Set("ipv6", vm.Nics[0].Ips[1])

		d.SetConnInfo(map[string]string{
			"type":     "ssh",
			"host":     vm.Nics[0].Ips[0],
			"password": d.Get("root_password").(string),
		})
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

const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

func randomPassword() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	b := make([]byte, passwordLength)
	for i := range b {
		b[i] = letters[r.Intn(len(letters))]
	}
	return string(b)
}
