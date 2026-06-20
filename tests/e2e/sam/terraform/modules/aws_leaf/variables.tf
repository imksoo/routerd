variable "run_id" { type = string }
variable "purpose" { type = string }
variable "commit" { type = string }
variable "expires_at" { type = string }
variable "managed_by" {
  type    = string
  default = "opentofu"
}
variable "vpc_id" { type = string }
variable "internet_gateway_id" { type = string }
variable "security_group_id" { type = string }
variable "iam_instance_profile" { type = string }
variable "name" {
  type    = string
  default = "aws-leaf"
}
variable "router_name" {
  type    = string
  default = null
}
variable "client_name" {
  type    = string
  default = null
}
variable "extra_leaf_nodes" {
  type = map(object({
    router_private_ip = string
    client_name       = string
    client_private_ip = string
  }))
  default = {}
}
variable "subnet_cidr" { type = string }
variable "router_private_ip" { type = string }
variable "client_private_ip" { type = string }
variable "ami_id" { type = string }
variable "instance_type" {
  type    = string
  default = "t3.medium"
}
variable "client_instance_type" {
  type    = string
  default = "t3.micro"
}
variable "key_name" { type = string }
