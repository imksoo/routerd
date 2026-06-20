variable "compartment_id" {
  type = string
  validation {
    condition     = var.compartment_id != ""
    error_message = "compartment_id is required. Must be the routerd-lab compartment, NOT ManagedCompartmentForPaaS."
  }
}
variable "availability_domain" { type = string }
variable "run_id" { type = string }
variable "purpose" { type = string }
variable "commit" { type = string }
variable "expires_at" { type = string }
variable "managed_by" {
  type    = string
  default = "opentofu"
}
variable "vcn_cidr" { type = string }
variable "subnet_cidr" { type = string }
variable "router_name" {
  type    = string
  default = "oci-leaf"
}
variable "client_name" {
  type    = string
  default = "oci-client"
}
variable "router_private_ip" { type = string }
variable "client_private_ip" { type = string }
variable "extra_leaf_nodes" {
  type = map(object({
    router_private_ip = string
    client_name       = string
    client_private_ip = string
  }))
  default = {}
}
variable "image_id" { type = string }
variable "shape" {
  type    = string
  default = "VM.Standard.E2.1.Micro"
}
variable "shape_ocpus" {
  type    = number
  default = null
}
variable "shape_memory_in_gbs" {
  type    = number
  default = null
}
variable "ssh_public_key" { type = string }
variable "ssh_cidr_blocks" {
  type    = list(string)
  default = ["0.0.0.0/0"]
}
