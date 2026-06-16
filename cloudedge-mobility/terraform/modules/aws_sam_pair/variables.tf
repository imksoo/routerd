variable "region" {
  type = string
}

variable "profile" {
  type = string
}

variable "vpc_cidr" {
  type = string
}

variable "subnet_cidr" {
  type = string
}

variable "ami_id" {
  type = string
}

variable "instance_type" {
  type    = string
  default = "t3.medium"
}

variable "key_name" {
  type = string
}

variable "iam_instance_profile" {
  type    = string
  default = ""
}

variable "capture_strategies" {
  type    = list(string)
  default = ["secondary-ip"]
  validation {
    condition     = alltrue([for s in var.capture_strategies : contains(["secondary-ip", "route-table"], s)])
    error_message = "capture_strategies must be a list of 'secondary-ip' and/or 'route-table'"
  }
}

variable "run_id" {
  type = string
}

variable "purpose" {
  type = string
}

variable "commit" {
  type = string
}

variable "expires_at" {
  type = string
}

variable "router_a_private_ip" {
  type = string
}

variable "router_b_private_ip" {
  type = string
}

variable "client_private_ip" {
  type = string
}

variable "ssh_cidr_blocks" {
  type    = list(string)
  default = ["0.0.0.0/0"]
}
