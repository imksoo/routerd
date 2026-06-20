locals {
  rr_nodes = var.topology_scale == "single" ? {
    aws-rr-a = { private_ip = "10.77.10.10" }
    } : {
    aws-rr-a = { private_ip = "10.77.10.10" }
    aws-rr-b = { private_ip = "10.77.10.11" }
  }

  aws_extra_leaf_nodes = var.topology_scale == "single" ? {} : {
    aws-leaf-b = {
      router_private_ip = "10.77.60.5"
      client_name       = "aws-client-b"
      client_private_ip = "10.77.60.16"
    }
  }

  azure_extra_leaf_nodes = var.topology_scale == "single" ? {} : {
    azure-leaf-b = {
      router_private_ip = "10.77.60.21"
      client_name       = "azure-client-b"
      client_private_ip = "10.77.60.17"
    }
  }

  oci_extra_leaf_nodes = var.topology_scale == "single" ? {} : {
    oci-leaf-b = {
      router_private_ip = "10.77.60.25"
      client_name       = "oci-client-b"
      client_private_ip = "10.77.60.18"
    }
  }

  pve_extra_leaf_nodes = var.topology_scale == "single" ? {} : {
    pve-leaf-b = {
      router_vm_id                = 112
      router_ipv4_cidr            = "10.77.60.35/24"
      router_management_ipv4_cidr = "192.168.1.135/24"
      client_name                 = "pve-client-b"
      client_vm_id                = 115
      client_ipv4_cidr            = "10.77.60.19/24"
      client_management_ipv4_cidr = "192.168.1.116/24"
    }
  }
}

module "aws_rr" {
  source = "../../modules/aws_rr"

  run_id          = var.run_id
  purpose         = var.purpose
  commit          = var.commit
  expires_at      = var.expires_at
  vpc_cidr        = "10.77.0.0/16"
  subnet_cidr     = "10.77.10.0/24"
  rr_nodes        = local.rr_nodes
  ami_id          = var.aws_ami_id
  instance_type   = "t3.medium"
  key_name        = var.aws_key_name
  ssh_cidr_blocks = var.ssh_cidr_blocks
}

module "aws_leaf" {
  source = "../../modules/aws_leaf"

  run_id               = var.run_id
  purpose              = var.purpose
  commit               = var.commit
  expires_at           = var.expires_at
  vpc_id               = module.aws_rr.vpc_id
  internet_gateway_id  = module.aws_rr.internet_gateway_id
  security_group_id    = module.aws_rr.security_group_id
  iam_instance_profile = module.aws_rr.iam_instance_profile
  router_name          = "aws-leaf-a"
  client_name          = "aws-client-a"
  subnet_cidr          = "10.77.60.0/24"
  router_private_ip    = "10.77.60.4"
  client_private_ip    = "10.77.60.11"
  extra_leaf_nodes     = local.aws_extra_leaf_nodes
  ami_id               = var.aws_ami_id
  instance_type        = "t3.medium"
  client_instance_type = "t3.micro"
  key_name             = var.aws_key_name
}

module "azure_leaf" {
  source = "../../modules/azure_leaf"

  location          = var.azure_location
  run_id            = var.run_id
  purpose           = var.purpose
  commit            = var.commit
  expires_at        = var.expires_at
  address_space     = "10.77.60.0/24"
  subnet_cidr       = "10.77.60.0/24"
  router_name       = "azure-leaf-a"
  client_name       = "azure-client-a"
  router_private_ip = "10.77.60.14"
  client_private_ip = "10.77.60.12"
  extra_leaf_nodes  = local.azure_extra_leaf_nodes
  admin_username    = var.azure_admin_username
  ssh_public_key    = var.ssh_public_key
  vm_size           = "Standard_B1s"
  ssh_cidr_blocks   = var.ssh_cidr_blocks
}

module "oci_leaf" {
  source = "../../modules/oci_leaf"

  compartment_id      = var.oci_compartment_id
  availability_domain = var.oci_availability_domain
  run_id              = var.run_id
  purpose             = var.purpose
  commit              = var.commit
  expires_at          = var.expires_at
  vcn_cidr            = "10.77.60.0/24"
  subnet_cidr         = "10.77.60.0/24"
  router_name         = "oci-leaf-a"
  client_name         = "oci-client-a"
  router_private_ip   = "10.77.60.24"
  client_private_ip   = "10.77.60.13"
  extra_leaf_nodes    = local.oci_extra_leaf_nodes
  image_id            = var.oci_image_id
  shape               = var.oci_shape
  shape_ocpus         = var.oci_shape_ocpus
  shape_memory_in_gbs = var.oci_shape_memory_in_gbs
  ssh_public_key      = var.ssh_public_key
  ssh_cidr_blocks     = var.ssh_cidr_blocks
}

module "pve_leaf" {
  source = "../../modules/pve_leaf"

  run_id                      = var.run_id
  purpose                     = var.purpose
  commit                      = var.commit
  expires_at                  = var.expires_at
  node_name                   = var.pve_node_name
  boot_source                 = var.pve_boot_source
  template_vm_id              = var.pve_template_vm_id
  iso_file_id                 = var.pve_iso_file_id
  iso_cdrom_interface         = var.pve_iso_cdrom_interface
  cloud_init_interface        = var.pve_cloud_init_interface
  datastore_id                = var.pve_datastore_id
  bridge                      = var.pve_underlay_bridge
  capture_bridge              = var.pve_capture_bridge
  vlan_id                     = var.pve_vlan_id
  router_name                 = "pve-leaf-a"
  client_name                 = "pve-client-a"
  router_ipv4_cidr            = "10.77.60.34/24"
  client_ipv4_cidr            = "10.77.60.15/24"
  router_management_ipv4_cidr = "192.168.1.134/24"
  client_management_ipv4_cidr = "192.168.1.115/24"
  extra_leaf_nodes            = local.pve_extra_leaf_nodes
  gateway_ipv4                = "192.168.1.1"
  ssh_public_key              = var.ssh_public_key
  username                    = var.pve_username
  router_vm_id                = var.pve_router_vm_id
  client_vm_id                = var.pve_client_vm_id
}
