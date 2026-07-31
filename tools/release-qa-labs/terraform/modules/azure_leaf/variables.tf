variable "location" { type = string }
variable "run_id" { type = string }
variable "purpose" { type = string }
variable "commit" { type = string }
variable "expires_at" { type = string }
variable "managed_by" {
  type    = string
  default = "opentofu"
}
variable "address_space" { type = string }
variable "subnet_cidr" { type = string }
variable "router_name" {
  type    = string
  default = "azure-leaf"
}
variable "client_name" {
  type    = string
  default = "azure-client"
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
variable "admin_username" {
  type    = string
  default = "ubuntu"
}
variable "ssh_public_key" { type = string }
variable "vm_size" {
  type    = string
  default = "Standard_B1s"
}
variable "ssh_cidr_blocks" {
  type    = list(string)
  default = ["0.0.0.0/0"]
}
