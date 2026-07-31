variable "run_id" { type = string }
variable "purpose" { type = string }
variable "commit" { type = string }
variable "expires_at" { type = string }
variable "managed_by" {
  type    = string
  default = "opentofu"
}
variable "vpc_cidr" { type = string }
variable "subnet_cidr" { type = string }
variable "rr_nodes" {
  type = map(object({
    private_ip = string
  }))
}
variable "ami_id" { type = string }
variable "instance_type" {
  type    = string
  default = "t3.medium"
}
variable "key_name" { type = string }
variable "ssh_cidr_blocks" {
  type    = list(string)
  default = ["0.0.0.0/0"]
}
