variable "run_id" { type = string }
variable "purpose" { type = string }
variable "commit" { type = string }
variable "expires_at" { type = string }
variable "node_name" { type = string }
variable "boot_source" {
  type    = string
  default = "template"
  validation {
    condition     = contains(["template", "iso"], var.boot_source)
    error_message = "boot_source must be either template or iso."
  }
}
variable "template_vm_id" {
  type    = number
  default = null
}
variable "template_source_node" {
  description = "Explicit PVE node that owns the run-scoped shared template. Required for template clones, including same-host leaves."
  type        = string
  default     = null
}
variable "clone_full" {
  description = "Template qualification requires full clones so every disposable workload has an independent disk."
  type        = bool
  default     = false
}
variable "iso_file_id" {
  type    = string
  default = null
}
variable "iso_cdrom_interface" {
  type    = string
  default = "ide2"
}
variable "cloud_init_interface" {
  type    = string
  default = null
}
variable "datastore_id" { type = string }
variable "bridge" { type = string }
variable "capture_bridge" {
  type    = string
  default = null
}
variable "vlan_id" {
  type    = number
  default = null
}
variable "router_name" {
  type    = string
  default = "pve-leaf"
}
variable "client_name" {
  type    = string
  default = "pve-client"
}
variable "router_ipv4_cidr" { type = string }
variable "client_ipv4_cidr" { type = string }
variable "extra_leaf_nodes" {
  type = map(object({
    router_vm_id     = number
    router_ipv4_cidr = string
    client_name      = string
    client_vm_id     = number
    client_ipv4_cidr = string
  }))
  default = {}
}
variable "capture_gateway_ipv4" {
  type    = string
  default = null
}
variable "ssh_public_key" { type = string }
variable "pve_ssh_host" {
  description = "Trusted PVE host FQDN used for QGA discovery of this module's guests."
  type        = string
}
variable "username" {
  type    = string
  default = "ubuntu"
}
variable "router_vm_id" {
  type    = number
  default = null
}
variable "client_vm_id" {
  type    = number
  default = null
}
variable "cpu_cores" {
  type    = number
  default = 2
}
variable "memory_mb" {
  type    = number
  default = 2048
}
variable "disk_gb" {
  type    = number
  default = 20
}
