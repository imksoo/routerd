provider "aws" {
  region  = var.aws_region
  profile = var.aws_profile
}

provider "azurerm" {
  features {}
  subscription_id = var.azure_subscription_id
}

provider "oci" {
  region              = var.oci_region
  config_file_profile = var.oci_profile
}

data "oci_identity_availability_domains" "provider_auth" {
  compartment_id = var.oci_compartment_id
}

provider "proxmox" {
  endpoint  = var.pve_endpoint
  api_token = var.pve_api_token
  insecure  = var.pve_insecure
}
