variable "run_id" { type = string }
variable "purpose" { type = string }
variable "commit" { type = string }
variable "expires_at" { type = string }

variable "rr_nodes" {
  description = "PVE RR nodes keyed by pve-rr-a and pve-rr-b. WireGuard bootstrap endpoints are generated inside each PVE guest from peer management addresses."
  type = map(object({
    pve_host        = string
    pve_ssh_host    = string
    vm_id           = number
    underlay_bridge = string
    vlan_id         = number
  }))
}

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
  description = "Explicit PVE node that owns the run-scoped shared template. Required for cross-host RR clones."
  type        = string
  default     = null
}
variable "clone_full" {
  description = "Template qualification requires full clones so every disposable RR has an independent disk."
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
variable "ssh_public_key" { type = string }
variable "username" {
  type    = string
  default = "ubuntu"
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
