terraform {
  required_version = ">= 1.8.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 6.0"
    }
    azurerm = {
      source  = "hashicorp/azurerm"
      version = ">= 4.0"
    }
    oci = {
      source  = "oracle/oci"
      version = ">= 6.0"
    }
    proxmox = {
      source  = "bpg/proxmox"
      version = ">= 0.75.0"
    }
  }
}
