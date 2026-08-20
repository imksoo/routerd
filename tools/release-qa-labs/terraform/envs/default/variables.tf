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
  description = "Run-scoped PVE API token (<pve.tokenOwner>!<run_id>=<secret>). Prefer the pinned TF_VAR input over tfvars."
  type        = string
  sensitive   = true
}
variable "pve_insecure" {
  type    = bool
  default = false
  validation {
    condition     = var.pve_insecure == false
    error_message = "pve_insecure must be false for release qualification; provide the pinned PVE CA instead."
  }
}
variable "pve_node_name" {
  description = "PVE cluster node to deploy VMs on."
  type        = string
}
variable "pve_rr_fault_domain" {
  description = "Assertion for the PVE RR fault domain. host-redundant requires distinct PVE hosts; cost-smoke labels an intentionally same-host RR pair and is rejected for topology_scale=full."
  type        = string
  default     = "host-redundant"
  validation {
    condition     = contains(["host-redundant", "cost-smoke"], var.pve_rr_fault_domain)
    error_message = "pve_rr_fault_domain must be either host-redundant or cost-smoke."
  }
}
variable "pve_rr_a_host" {
  description = "Short PVE cluster node ID that hosts pve-rr-a."
  type        = string
  validation {
    condition     = try(trimspace(var.pve_rr_a_host) != "", false)
    error_message = "pve_rr_a_host must be non-empty."
  }
}
variable "pve_rr_a_ssh_host" {
  description = "PVE host FQDN used to inspect pve-rr-a."
  type        = string
  validation {
    condition     = try(trimspace(var.pve_rr_a_ssh_host) != "", false)
    error_message = "pve_rr_a_ssh_host must be non-empty."
  }
}
variable "pve_rr_a_vm_id" {
  type = number
  validation {
    condition     = try(var.pve_rr_a_vm_id > 0, false)
    error_message = "pve_rr_a_vm_id must be positive."
  }
}
variable "pve_rr_a_underlay_bridge" {
  description = "Optional PVE bridge for pve-rr-a; defaults to pve_underlay_bridge."
  type        = string
  default     = null
  validation {
    condition     = try(var.pve_rr_a_underlay_bridge == null || trimspace(var.pve_rr_a_underlay_bridge) != "", true)
    error_message = "pve_rr_a_underlay_bridge must be non-empty when set."
  }
}
variable "pve_rr_a_vlan_id" {
  description = "Optional VLAN ID for pve-rr-a; defaults to pve_vlan_id."
  type        = number
  default     = null
  validation {
    condition     = try(var.pve_rr_a_vlan_id == null || (var.pve_rr_a_vlan_id >= 1 && var.pve_rr_a_vlan_id <= 4094), true)
    error_message = "pve_rr_a_vlan_id must be in 1..4094 when set."
  }
}

variable "pve_rr_b_host" {
  description = "Short PVE cluster node ID that hosts pve-rr-b. Required for topology_scale=full."
  type        = string
  default     = null
  validation {
    condition     = try(var.pve_rr_b_host == null || trimspace(var.pve_rr_b_host) != "", true)
    error_message = "pve_rr_b_host must be non-empty when set."
  }
}
variable "pve_rr_b_ssh_host" {
  description = "PVE host FQDN used to inspect pve-rr-b."
  type        = string
  default     = null
  validation {
    condition     = try(var.pve_rr_b_ssh_host == null || trimspace(var.pve_rr_b_ssh_host) != "", true)
    error_message = "pve_rr_b_ssh_host must be non-empty when set."
  }
}
variable "pve_rr_b_vm_id" {
  type    = number
  default = null
  validation {
    condition     = try(var.pve_rr_b_vm_id == null || var.pve_rr_b_vm_id > 0, true)
    error_message = "pve_rr_b_vm_id must be positive when set."
  }
}
variable "pve_rr_b_underlay_bridge" {
  description = "Optional PVE bridge for pve-rr-b; defaults to pve_underlay_bridge."
  type        = string
  default     = null
  validation {
    condition     = try(var.pve_rr_b_underlay_bridge == null || trimspace(var.pve_rr_b_underlay_bridge) != "", true)
    error_message = "pve_rr_b_underlay_bridge must be non-empty when set."
  }
}
variable "pve_rr_b_vlan_id" {
  description = "Optional VLAN ID for pve-rr-b; defaults to pve_vlan_id."
  type        = number
  default     = null
  validation {
    condition     = try(var.pve_rr_b_vlan_id == null || (var.pve_rr_b_vlan_id >= 1 && var.pve_rr_b_vlan_id <= 4094), true)
    error_message = "pve_rr_b_vlan_id must be in 1..4094 when set."
  }
}
variable "pve_ssh_host" {
  description = "DNS FQDN used for PVE SSH and TCP connectivity."
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
  description = "Existing immutable source template VMID. The release run copies it to a disposable shared-storage template before cloning any workload."
  type        = number
  default     = null
}
variable "pve_template_source_node" {
  description = "PVE node that owns pve_template_vm_id and creates the run-scoped shared template."
  type        = string
  default     = null
}
variable "pve_template_stage_vm_id" {
  description = "Disposable, run-scoped VMID used only for the full shared-storage template stage."
  type        = number
  default     = null
  validation {
    condition     = try(var.pve_template_stage_vm_id == null || var.pve_template_stage_vm_id > 0, true)
    error_message = "pve_template_stage_vm_id must be positive when set."
  }
}
variable "pve_clone_full" {
  description = "Use full clones from the run-scoped shared template. Release qualification requires true."
  type        = bool
  default     = false
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
  validation {
    condition     = try(var.pve_router_vm_id == null || var.pve_router_vm_id > 0, true)
    error_message = "pve_router_vm_id must be positive when set."
  }
}
variable "pve_client_vm_id" {
  type    = number
  default = null
  validation {
    condition     = try(var.pve_client_vm_id == null || var.pve_client_vm_id > 0, true)
    error_message = "pve_client_vm_id must be positive when set."
  }
}
variable "pve_leaf_b_router_vm_id" {
  description = "Explicit VM ID for pve-leaf-b. Required for topology_scale=full."
  type        = number
  default     = null
  validation {
    condition     = try(var.pve_leaf_b_router_vm_id == null || var.pve_leaf_b_router_vm_id > 0, true)
    error_message = "pve_leaf_b_router_vm_id must be positive when set."
  }
}
variable "pve_leaf_b_client_vm_id" {
  description = "Explicit VM ID for pve-client-b. Required for topology_scale=full."
  type        = number
  default     = null
  validation {
    condition     = try(var.pve_leaf_b_client_vm_id == null || var.pve_leaf_b_client_vm_id > 0, true)
    error_message = "pve_leaf_b_client_vm_id must be positive when set."
  }
}
