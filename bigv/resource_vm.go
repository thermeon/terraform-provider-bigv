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
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/terraform/helper/schema"
)

const passwordLength = 20

type bigvVm struct {
	Id           int    `json:"id,omitempty"`
	Name         string `json:"name,omitempty"`
	Cores        int    `json:"cores,omitempty"`
	Memory       int    `json:"memory,omitempty"`
	Hostname     string `json:"hostname,omitempty"`
	Distribution string `json:"last_imaged_with,omitempty"`
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

type bigvNetwork struct {
	// Create attributes
	Ipv4 string `json:"ipv4,omitempty"`
	Ipv6 string `json:"ipv6,omitempty"`

	// Read Attributes
	Label string   `json:"label,omitempty"`
	Ips   []string `json:"ips,omitempty"`
	Mac   string   `json:"mac,omitempty"`
}

type bigvServer struct {
	VirtualMachine bigvVm      `json:"virtual_machine"`
	Discs          []bigvDisc  `json:"discs,omitempty"`
	Image          bigvImage   `json:"reimage,omitempty"`
	Network        bigvNetwork `json:"ips,omitempty"`
}

type bigvNic struct {
}

func resourceBigvVM() *schema.Resource {
	return &schema.Resource{
		Create: resourceBigvVMCreate,
		Read:   resourceBigvVMRead,
		Update: resourceBigvVMUpdate,
		Delete: resourceBigvVMDelete,
		Schema: map[string]*schema.Schema{
			"name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"ip": &schema.Schema{
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
		},
	}
}

func resourceBigvVMCreate(d *schema.ResourceData, meta interface{}) error {

	l := log.New(os.Stderr, "", 0)

	bigvClient := meta.(*client)

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
		Image: bigvImage{
			Distribution: d.Get("os").(string),
			RootPassword: randomPassword(),
		},
		Network: bigvNetwork{
			Ipv4: d.Get("ip").(string),
		},
	}

	if err := vm.VirtualMachine.validateCoresToMemory(); err != nil {
		return err
	}

	body, err := json.Marshal(vm)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/vm_create", bigvClient.uri())

	l.Printf("Requesting VM create: %s", url)
	l.Printf("VM profile: %s", body)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		l.Printf("Error creating http request!")
		return err
	}

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

		if body, err := ioutil.ReadAll(resp.Body); err != nil {
			return err
		} else {
			// The first read we do just parses what they've immediately told us
			// That excludes the ip address only
			if err := resourceFromJson(d, body); err != nil {
				return err
			}
		}

		l.Printf("Created BigV VM, Id: %s", d.Id())

		return nil
	}

}

func resourceBigvVMUpdate(d *schema.ResourceData, meta interface{}) error {
	l := log.New(os.Stderr, "", 0)

	bigvClient := meta.(*client)

	vm := bigvVm{}

	if d.HasChange("cores") || d.HasChange("memory") {
		// Specifiy both cores and memory together always, so we can validate them.
		vm.Cores = d.Get("cores").(int)
		vm.Memory = d.Get("memory").(int)
	}

	if err := vm.validateCoresToMemory(); err != nil {
		return err
	}

	body, err := json.Marshal(vm)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/virtual_machines/%s",
		bigvClient.uri(),
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
			// This time we only get back the virtual_machine section
			jsonStr := []byte(fmt.Sprintf(`{"virtual_machine": %s}`,
				body,
			))

			if err := resourceFromJson(d, jsonStr); err != nil {
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

	var wg sync.WaitGroup

	type jobSpec struct {
		uri  string
		resp []byte
		err  error
	}

	tasks := map[string]*jobSpec{
		"machine": {uri: ""},
		"discs":   {uri: "/discs"},
		"ips":     {uri: "/nics"},
	}

	for name, task := range tasks {
		wg.Add(1)
		go func(j *jobSpec) {
			defer wg.Done()

			url := fmt.Sprintf("%s/virtual_machines/%s%s",
				bigvClient.uri(),
				d.Id(),
				j.uri,
			)

			l.Printf("Request VM Read of %s from %s", name, url)

			req, _ := http.NewRequest("GET", url, nil)

			if resp, err := bigvClient.do(req); err != nil {
				j.err = err
			} else {

				// Always close the body when done
				defer resp.Body.Close()

				l.Printf("HTTP response Status: %s", resp.Status)

				if resp.StatusCode != http.StatusOK {
					j.err = fmt.Errorf("Read VM %s Bad HTTP status from bigv: %d", name, resp.StatusCode)
				}

				if body, err := ioutil.ReadAll(resp.Body); err != nil {
					j.err = err
				} else {
					j.resp = bytes.TrimSpace(body)
				}
			}
		}(task)
	}

	wg.Wait()

	// Collect errors
	{
		var errs []string
		for _, task := range tasks {
			if task.err != nil {
				errs = append(errs, task.err.Error())
			}
		}
		if len(errs) > 0 {
			return errors.New(strings.Join(errs, "\n"))
		}
	}

	jsonStr := []byte(fmt.Sprintf(`{"virtual_machine": %s, "discs": %s, "ips": %s}`,
		tasks["machine"].resp,
		tasks["discs"].resp,
		// Remove the array of ips. God help us if we get mulitple nics. TODO, I guess.
		bytes.TrimRight(bytes.TrimLeft(tasks["ips"].resp, "["), "]"),
	))

	return resourceFromJson(d, jsonStr)
}

func resourceBigvVMDelete(d *schema.ResourceData, meta interface{}) error {

	bigvClient := meta.(*client)

	url := fmt.Sprintf("%s/virtual_machines/%s?purge=true",
		bigvClient.uri(),
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

func resourceFromJson(d *schema.ResourceData, vmJson []byte) error {
	l := log.New(os.Stderr, "", 0)

	l.Printf("VM definition: %s", vmJson)

	vm := &bigvServer{}

	if err := json.Unmarshal(vmJson, vm); err != nil {
		return err
	}

	d.SetId(strconv.Itoa(vm.VirtualMachine.Id))
	d.Set("name", vm.VirtualMachine.Name)
	d.Set("cores", vm.VirtualMachine.Cores)
	d.Set("memory", vm.VirtualMachine.Memory)

	// If we don't get discs back, this was probably an update request
	if len(vm.Discs) == 1 {
		d.Set("disk_size", vm.Discs[0].Size)
	}

	// Root password is never sent back to us
	if vm.Image.RootPassword != "" {
		d.Set("root_password", vm.Image.RootPassword)
	}

	// Distribution is empty in create response, leave it with what we sent in
	if vm.VirtualMachine.Distribution != "" {
		d.Set("os", vm.VirtualMachine.Distribution)
	}

	// Not finding the ips is fine, because they're not sent back in the create request
	if len(vm.Network.Ips) > 0 {
		d.Set("ip", vm.Network.Ips[0])
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
