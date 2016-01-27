package bigv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"

	"github.com/hashicorp/terraform/helper/schema"
)

const passwordLength = 20

type bigvVm struct {
	Id       int    `json:"id,omitempty"`
	Name     string `json:"name"`
	Cores    int    `json:"cores"`
	Memory   int    `json:"memory"`
	Hostname string `json:"hostname,omitempty"`
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
		Read:   resourceBigvVMRead,
		//Update: resourceBigvVMUpdate,
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

	l := log.New(os.Stderr, "", 0)

	bigvConfig := meta.(*config)

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
		Ips: bigvNetwork{
			Ipv4: "46.43.49.201",
		},
	}

	body, err := json.Marshal(vm)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://uk0.bigv.io/accounts/%s/groups/%s/vm_create", bigvConfig.account, bigvConfig.group)
	l.Printf("Requesting create: %s", url)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(bigvConfig.user, bigvConfig.password)

	vmCreated := &bigvServer{}
	client := &http.Client{}

	if resp, err := client.Do(req); err != nil {
		return err
	} else {

		l.Printf("HTTP response Status: %s", resp.Status)

		if resp.StatusCode != http.StatusAccepted {
			return fmt.Errorf("Bad HTTP resposne status from bigv: %d", resp.StatusCode)
		}

		if body, err := ioutil.ReadAll(resp.Body); err != nil {
			return err
		} else {
			if err = json.Unmarshal(body, vmCreated); err != nil {
				return err
			}
		}
	}

	d.SetId(strconv.Itoa(vmCreated.VirtualMachine.Id))

	l.Printf("Created BigV VM, Id: %s", d.Id())

	return nil
}

func resourceBigvVMUpdate(d *schema.ResourceData, meta interface{}) error {
	fmt.Fprintln(os.Stderr, "Begin a update run")

	return nil
}

func resourceBigvVMRead(d *schema.ResourceData, meta interface{}) error {
	fmt.Fprintln(os.Stderr, "Begin a read run")

	return nil
}

func resourceBigvVMDelete(d *schema.ResourceData, meta interface{}) error {

	fmt.Fprintln(os.Stderr, "Begin a delete run")

	bigvConfig := meta.(*config)

	url := fmt.Sprintf("https://uk0.bigv.io/accounts/%s/groups/%s/virtual_machines/%s",
		bigvConfig.account,
		bigvConfig.group,
		d.Id(),
	)
	fmt.Fprintln(os.Stderr, "To url for delete: %s", url)
	req, err := http.NewRequest("DELETE", url, nil)
	req.SetBasicAuth(bigvConfig.user, bigvConfig.password)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	fmt.Fprintln(os.Stderr, "response Status:", resp.Status)
	fmt.Fprintln(os.Stderr, "response Headers:", resp.Header)

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
