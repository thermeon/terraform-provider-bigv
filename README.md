# Terraform-Profider-Bigv

[BigV](http://bigv.io) is a private dedicated virtual machine platfom provided by Bytemark,

This terraform provider allows the management of machines on that platform.

See [BigV prices](http://www.bigv.io/prices), [Resource definitions](http://www.bigv.io/support/api/definitions/) and 
[API documentation](http://www.bigv.io/support/api/virtual_machines/) for configuration options to resource parameters.


## Resource changes and reboots

If you change cores or memory, the VM *will* be restarted.
It's always necessary for increases. Strictly bigv can downsize without restarts,
but we've noticed that decreasing RAM nearly always ends up inconsistent in the VM.
e.g. decreasing to 1GiB gives you 750MiB until you restart.

## Provider parameters

* **account**

   Bigv account name.


* **user**

   Bigv Username

* **password**

   Your bigv password.
   Yubikey is not yet supported. Patches welcome.

## Resource parameters

* **name**

   The VM name

* **group**

   The group name for the server.

   Defaults to default.

* **zone**

   The zone to put the server in. Currently this is machester or york. See [definitions](http://www.bigv.io/support/api/definitions/).

   Defaults to york.

* **cores**

   How many cores to allocate. Note that cores must be allocated 1 per 4GiB of RAM.
   So 1-3GiB = 1 core, 4-7GiB = 2 core.

   If you don't specify cores, but do specify memory, you'll be assigned the correct cores for the memory.

   Defaults to 1.

* **memory**

   How much RAM to allocate in MiB.

   If you don't specify mmeory byt do specify cores, you'll be allocated the minimal memory for the core count.
   e.g. 1GiB for 1 core, 4GiB for 2 cores, etc.

   Defaults to 1024.

* **os**

   Short name of operating system to image.
   See: [Resource definitions](http://www.bigv.io/support/api/definitions/)
   Common options are: jessie, vivid and none

   Defaults to none.

* **ipv4**
* **ipv6**

   IP address to allocate.
   We force ipv6 to be specified because it eases the burden on bytemark's allocation process,
   and should allow concurrent imaging without deadlocks.

* **disc_size**

   Disc size in MiB. More options in the API are not yet supported, such as storage grade.

   Defaults to 25600

* **power_on**

   Whether or not the machine should be powered.
   Note that if *reboot* is true then setting *power_on* to false just reboots the VM immediately.
   See [Bigv power documentation](http://www.bigv.io/support/api/virtual_machines/#Power)
   
   Defaults to true.

* **reboot**

   Whether or not the machine should be rebooted automatically when powered down.
   See [Bigv power documentation](http://www.bigv.io/support/api/virtual_machines/#Power)
   
   Defaults to true.

* **ssh_public_key**

   SSH public key to be created on the VM. Can be multiple keys.

## Computed values

* **root_password**

   The root password assigned to this vm.

## Example Usage

variables.tf:
```
variable "account" {
  default = "myaccount"
}
variable "user" {
  default = "myuser"
}
// Probably best to supply this interactively
variable "password" { }
```

provider.tf:
```
provider "bigv" {
  account  = "${var.account}"
  user     = "${var.user}"
  password = "${var.password}"
}
```

resources.tf:
```
resource "bigv_vm" "tf01" {
  name    = "tf01"
  ipv4    = "49.48.12.201"
  ipv6    = "1996:41c8:20:5ed::3:1"
  cores   = 1
  memory  = 1024
  os      = "vivid"
  group   = "default"
  zone    = "manchester"
}
 
resource "bigv_vm" "tf02" {
  name    = "tf02"
  ipv4    = "49.48.12.202"
  ipv6    = "1996:41c8:20:5ed::3:2"
  cores   = 2
  memory  = 4096
  os      = "trusty"
  group   = "vlan1519"
  zone    = "york"
}
 
resource "bigv_vm" "tf03" {
  name    = "tf03"
  ipv4     = "49.48.12.203"
  ipv6    = "1996:41c8:20:5ed::3:3"
  cores   = 1
  memory  = 1024
  os      = "none"
} 
```

And then just:
```
terraform plan
terraform apply
terraform refresh
terraform destroy
```

## Debugging and troubleshooting

Terraform commands support TF_LOG=level environment variables:
```
TF_LOG=DEBUG terraform apply
```
This provider will be fairly verbose.
