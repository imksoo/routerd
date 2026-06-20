# --- Run metadata ---

variable "run_id" {
  description = "Unique identifier for this test run (e.g. sam-e2e-20260620-0200)."
  type        = string
}
variable "purpose" {
  type    = string
  default = "sam-e2e-standard-client-to-client-hostname"
}
variable "commit" {
  description = "routerd version or git commit under test."
  type        = string
}
variable "expires_at" {
  description = "ISO 8601 timestamp after which resources should be destroyed."
  type        = string
}

variable "topology_scale" {
  description = "SAM topology scale. Use single for the first low-cost apply, full for the target 2RR/8leaf matrix."
  type        = string
  default     = "full"
  validation {
    condition     = contains(["single", "full"], var.topology_scale)
    error_message = "topology_scale must be either single or full."
  }
}

# --- SSH ---

variable "ssh_public_key" {
  description = "SSH public key deployed to all nodes."
  type        = string
}
variable "ssh_cidr_blocks" {
  type    = list(string)
  default = ["0.0.0.0/0"]
}

# --- AWS ---

variable "aws_profile" {
  description = "AWS CLI profile name (aws configure --profile <name>)."
  type        = string
  default     = "default"
}
variable "aws_region" {
  type    = string
  default = "ap-northeast-1"
}
variable "aws_key_name" {
  description = "EC2 Key Pair name registered in the target region."
  type        = string
}
variable "aws_ami_id" {
  description = "Ubuntu AMI ID for the target region."
  type        = string
}

# --- Azure ---

variable "azure_subscription_id" {
  description = "Azure Subscription ID (az account show --query id)."
  type        = string
}
variable "azure_location" {
  type    = string
  default = "japaneast"
}
variable "azure_admin_username" {
  type    = string
  default = "ubuntu"
}

# --- OCI ---

variable "oci_profile" {
  description = "OCI CLI config profile name (~/.oci/config)."
  type        = string
  default     = "DEFAULT"
}
variable "oci_region" {
  type    = string
  default = "ap-tokyo-1"
}
variable "oci_compartment_id" {
  description = "OCI compartment OCID. Must NOT be ManagedCompartmentForPaaS."
  type        = string
  validation {
    condition     = var.oci_compartment_id != ""
    error_message = "oci_compartment_id is required. Verify it is the routerd-lab compartment, not ManagedCompartmentForPaaS."
  }
}
variable "oci_availability_domain" { type = string }
variable "oci_image_id" {
  description = "Ubuntu image OCID for the target region."
  type        = string
}
variable "oci_shape" {
  type    = string
  default = "VM.Standard.E2.1"
}
variable "oci_shape_ocpus" {
  type    = number
  default = null
}
variable "oci_shape_memory_in_gbs" {
  type    = number
  default = null
}

# --- Proxmox VE ---

variable "pve_endpoint" {
  description = "PVE API URL (e.g. https://pve01.local:8006/)."
  type        = string
}
variable "pve_api_token" {
  description = "PVE API token. Prefer TF_VAR_pve_api_token env var over tfvars."
  type        = string
  sensitive   = true
}
variable "pve_insecure" {
  type    = bool
  default = true
}
variable "pve_node_name" {
  description = "PVE cluster node to deploy VMs on."
  type        = string
}
variable "pve_boot_source" {
  type    = string
  default = "template"
  validation {
    condition     = contains(["template", "iso"], var.pve_boot_source)
    error_message = "pve_boot_source must be either template or iso."
  }
}
variable "pve_template_vm_id" {
  type    = number
  default = null
}
variable "pve_iso_file_id" {
  type    = string
  default = null
}
variable "pve_iso_cdrom_interface" {
  type    = string
  default = "ide2"
}
variable "pve_cloud_init_interface" {
  type    = string
  default = null
}
variable "pve_datastore_id" { type = string }
variable "pve_underlay_bridge" { type = string }
variable "pve_capture_bridge" {
  type    = string
  default = null
}
variable "pve_vlan_id" {
  type    = number
  default = null
}
variable "pve_username" {
  type    = string
  default = "ubuntu"
}
variable "pve_router_vm_id" {
  type    = number
  default = null
}
variable "pve_client_vm_id" {
  type    = number
  default = null
}
