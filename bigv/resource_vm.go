package bigv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
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
	Name         string `json:"name"`
	Cores        int    `json:"cores"`
	Memory       int    `json:"memory"`
	Hostname     string `json:"hostname,omitempty"`
	Distribution string `json:"last_imaged_with,omitempty"`
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
	// Create attributes
	Ipv4 string `json:"ipv4,omitempty"`
	Ipv6 string `json:"ipv6,omitempty"`

	// Read Attributes
	Label string   `json:"label"`
	Ips   []string `json:"ips"`
	Mac   string   `json:"mac"`
}

type bigvServer struct {
	VirtualMachine bigvVm      `json:"virtual_machine"`
	Discs          []bigvDisc  `json:"discs"`
	Image          bigvImage   `json:"reimage"`
	Network        bigvNetwork `json:"ips"`
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

	body, err := json.Marshal(vm)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/vm_create", bigvClient.uri())

	l.Printf("Requesting VM create: %s", url)
	l.Printf("VM profile: %s", body)

	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(bigvClient.user, bigvClient.password)

	client := &http.Client{}

	if resp, err := client.Do(req); err != nil {
		return err
	} else {

		l.Printf("HTTP response Status: %s", resp.Status)

		if resp.StatusCode != http.StatusAccepted {
			return fmt.Errorf("Create VM bad status from bigv: %d", resp.StatusCode)
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

	vm := bigvServer{}

	if id, err := strconv.Atoi(d.Id()); err != nil {
		return err
	} else {
		vm.VirtualMachine.Id = id
	}

	if d.HasChange("cores") {
		vm.VirtualMachine.Cores = d.Get("cores").(int)
	}

	if d.HasChange("memory") {
		vm.VirtualMachine.Memory = d.Get("memory").(int)
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
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(bigvClient.user, bigvClient.password)

	client := &http.Client{}

	if resp, err := client.Do(req); err != nil {
		return err
	} else {

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
			req.SetBasicAuth(bigvClient.user, bigvClient.password)

			client := &http.Client{}

			if resp, err := client.Do(req); err != nil {
				j.err = err
			} else {

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
	req.SetBasicAuth(bigvClient.user, bigvClient.password)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

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

const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*()-_=+,.?/:;{}[]`~"

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
 * Allocate ip automatically
 * Set id properly
 * Delete error handling
 */
