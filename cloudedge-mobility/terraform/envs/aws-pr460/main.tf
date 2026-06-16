module "aws_sam_pair" {
  source = "../../modules/aws_sam_pair"

  region               = var.region
  profile              = var.profile
  vpc_cidr             = "10.77.60.0/24"
  subnet_cidr          = "10.77.60.0/24"
  ami_id               = "ami-05f4eb3328c0dabc5"
  instance_type        = "t3.medium"
  key_name             = "routerd-cloudedge-lab-20260529"
  capture_strategies   = ["secondary-ip", "route-table"]
  run_id               = var.run_id
  purpose              = var.purpose
  commit               = var.commit
  expires_at           = var.expires_at
  router_a_private_ip  = "10.77.60.4"
  router_b_private_ip  = "10.77.60.6"
  client_private_ip    = "10.77.60.11"
  ssh_cidr_blocks      = var.ssh_cidr_blocks
}
