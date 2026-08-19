locals {
  # Match the leaf module's generated-name fallback so RR topology checks
  # remain fail-closed even when a non-release cost-smoke omits the explicit
  # capture bridge value.
  effective_pve_capture_bridge = coalesce(var.pve_capture_bridge, "rsam${substr(md5(var.run_id), 0, 6)}")

  # A reviewed full topology always names each of these six VMs explicitly.
  # Keep the list in one place so the plan rejects an accidental VMID collision
  # even when it is invoked outside the release-contract guard.
  pve_full_vm_ids = [
    var.pve_router_vm_id,
    var.pve_client_vm_id,
    var.pve_leaf_b_router_vm_id,
    var.pve_leaf_b_client_vm_id,
    var.pve_rr_a_vm_id,
    var.pve_rr_b_vm_id,
  ]

  # Every template-backed PVE workload is cloned from this run-scoped,
  # full-copy template. The original template may remain on local storage; the
  # stage makes its disks available on the configured shared datastore before
  # any RR on another host is created.
  pve_clone_template_vm_id = var.pve_boot_source == "template" ? proxmox_virtual_environment_vm.pve_shared_template_stage[0].vm_id : var.pve_template_vm_id
  pve_clone_source_node    = var.pve_boot_source == "template" ? proxmox_virtual_environment_vm.pve_shared_template_stage[0].node_name : null

  pve_rr_nodes = merge(
    {
      pve-rr-a = {
        pve_host        = var.pve_rr_a_host
        pve_ssh_host    = var.pve_rr_a_ssh_host
        vm_id           = var.pve_rr_a_vm_id
        underlay_bridge = var.pve_rr_a_underlay_bridge != null ? var.pve_rr_a_underlay_bridge : var.pve_underlay_bridge
        vlan_id         = var.pve_rr_a_vlan_id != null ? var.pve_rr_a_vlan_id : var.pve_vlan_id
      }
    },
    var.pve_rr_b_host == null ? {} : {
      pve-rr-b = {
        pve_host        = var.pve_rr_b_host
        pve_ssh_host    = var.pve_rr_b_ssh_host
        vm_id           = var.pve_rr_b_vm_id
        underlay_bridge = var.pve_rr_b_underlay_bridge != null ? var.pve_rr_b_underlay_bridge : var.pve_underlay_bridge
        vlan_id         = var.pve_rr_b_vlan_id != null ? var.pve_rr_b_vlan_id : var.pve_vlan_id
      }
    }
  )

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
      router_vm_id     = var.pve_leaf_b_router_vm_id
      router_ipv4_cidr = "10.77.60.35/24"
      client_name      = "pve-client-b"
      client_vm_id     = var.pve_leaf_b_client_vm_id
      client_ipv4_cidr = "10.77.60.19/24"
    }
  }
}

resource "terraform_data" "pve_rr_topology" {
  input = {
    topology_scale       = var.topology_scale
    fault_domain         = var.pve_rr_fault_domain
    rr_nodes             = local.pve_rr_nodes
    expected_full_rr_set = ["pve-rr-a", "pve-rr-b"]
  }

  lifecycle {
    precondition {
      condition     = var.pve_rr_a_host != null && var.pve_rr_a_ssh_host != null && var.pve_rr_a_vm_id != null
      error_message = "pve-rr-a requires explicit host, PVE SSH host, and VM ID. QGA discovers its DHCP management address after PVE apply."
    }

    precondition {
      condition = (
        var.pve_rr_b_host == null &&
        var.pve_rr_b_ssh_host == null &&
        var.pve_rr_b_vm_id == null
        ) || (
        var.pve_rr_b_host != null &&
        var.pve_rr_b_ssh_host != null &&
        var.pve_rr_b_vm_id != null
      )
      error_message = "pve-rr-b must either be omitted completely or define host, PVE SSH host, and VM ID together."
    }

    precondition {
      condition     = var.pve_rr_b_host == null || var.pve_rr_a_vm_id != var.pve_rr_b_vm_id
      error_message = "pve-rr-a and pve-rr-b must use distinct VM IDs."
    }

    precondition {
      condition = var.topology_scale != "full" || (
        var.pve_router_vm_id != null &&
        var.pve_client_vm_id != null &&
        var.pve_leaf_b_router_vm_id != null &&
        var.pve_leaf_b_client_vm_id != null
      )
      error_message = "topology_scale=full requires explicit VM IDs for both PVE leaf/client pairs."
    }

    precondition {
      condition     = var.topology_scale != "full" || length(distinct(local.pve_full_vm_ids)) == length(local.pve_full_vm_ids)
      error_message = "topology_scale=full requires six distinct PVE VM IDs across both leaf/client pairs and the two route reflectors."
    }

    precondition {
      condition = var.pve_boot_source != "template" || (
        var.pve_template_vm_id != null &&
        var.pve_template_source_node == var.pve_node_name &&
        var.pve_template_stage_vm_id != null &&
        var.pve_template_stage_vm_id != var.pve_template_vm_id &&
        !contains(local.pve_full_vm_ids, var.pve_template_stage_vm_id) &&
        !contains(local.pve_full_vm_ids, var.pve_template_vm_id) &&
        var.pve_clone_full
      )
      error_message = "template boot requires a distinct, run-scoped shared-template VMID on the leaf/source node and full clones; the immutable source template may not overlap a workload VMID."
    }

    precondition {
      condition = var.pve_rr_fault_domain != "host-redundant" || (
        length(local.pve_rr_nodes) == 2 &&
        length(distinct([for node in values(local.pve_rr_nodes) : node.pve_host])) == 2 &&
        length(distinct([for node in values(local.pve_rr_nodes) : node.pve_ssh_host])) == 2
      )
      error_message = "pve_rr_fault_domain=host-redundant requires exactly two RRs on distinct PVE hosts and distinct PVE SSH hosts. Use cost-smoke to label a one-RR or intentionally same-host configuration."
    }

    precondition {
      condition     = trimspace(var.pve_underlay_bridge) != local.effective_pve_capture_bridge
      error_message = "PVE leaf underlay bridge must not equal the leaf capture bridge. Leaf management/underlay and capture stay on separate NICs."
    }

    precondition {
      condition     = alltrue([for node in values(local.pve_rr_nodes) : trimspace(node.underlay_bridge) != local.effective_pve_capture_bridge])
      error_message = "PVE RR underlay bridges must not equal the leaf capture bridge. RRs are management/underlay-only and may not have a capture NIC."
    }

    precondition {
      condition = var.topology_scale != "full" || (
        var.pve_rr_fault_domain == "host-redundant" &&
        length(local.pve_rr_nodes) == 2 &&
        # The cardinality assertion plus both explicit membership checks is
        # equivalent to set equality, while remaining compatible with the
        # OpenTofu version used by the qualification host.
        contains(keys(local.pve_rr_nodes), "pve-rr-a") &&
        contains(keys(local.pve_rr_nodes), "pve-rr-b") &&
        length(distinct([for node in values(local.pve_rr_nodes) : node.pve_host])) == 2 &&
        length(distinct([for node in values(local.pve_rr_nodes) : node.pve_ssh_host])) == 2
      )
      error_message = "topology_scale=full requires exactly pve-rr-a and pve-rr-b on distinct PVE hosts and PVE SSH hosts with pve_rr_fault_domain=host-redundant."
    }
  }
}

# The PVE 9.2 clone API only permits a cross-host clone when the source VM is
# on shared storage. VM 9000 is intentionally an immutable local template, so
# first create an unstarted, run-scoped full-copy template on qnap (or another
# preflight-certified shared datastore). Terraform's dependency graph then
# destroys the six dependent VMs before this stage template during cleanup.
resource "proxmox_virtual_environment_vm" "pve_shared_template_stage" {
  count       = var.pve_boot_source == "template" ? 1 : 0
  name        = "routerd-${var.run_id}-pve-template-stage"
  description = "routerd SAM E2E shared template stage; run=${var.run_id}; purpose=${var.purpose}; commit=${var.commit}; expires=${var.expires_at}; disposable=true"
  node_name   = var.pve_template_source_node
  vm_id       = var.pve_template_stage_vm_id
  # Proxmox canonicalizes tags to lower case.  Keep the desired value in that
  # form so the stage-only apply cannot create a spurious update in the full
  # PVE plan.
  tags     = ["routerd", "sam-e2e", lower(var.run_id), "template-stage"]
  started  = false
  template = true

  clone {
    vm_id        = var.pve_template_vm_id
    node_name    = var.pve_template_source_node
    datastore_id = var.pve_datastore_id
    full         = true
  }

  lifecycle {
    precondition {
      condition = (
        var.pve_template_vm_id != null &&
        var.pve_template_source_node != null &&
        var.pve_template_stage_vm_id != null &&
        var.pve_template_stage_vm_id != var.pve_template_vm_id &&
        var.pve_clone_full
      )
      error_message = "the shared template stage requires source/template VMIDs, a source node, and full workload clones."
    }
  }
}

# Release qualification is fresh-state only. Do not add old AWS RR state
# migration aliases here: the contract rejects legacy state before any plan
# or provider operation, and the pre-apply inventory must already be zero.

module "aws_fabric" {
  source = "../../modules/aws_fabric"

  run_id          = var.run_id
  purpose         = var.purpose
  commit          = var.commit
  expires_at      = var.expires_at
  vpc_cidr        = "10.77.0.0/16"
  ssh_cidr_blocks = var.ssh_cidr_blocks
}

module "aws_leaf" {
  source = "../../modules/aws_leaf"

  run_id               = var.run_id
  purpose              = var.purpose
  commit               = var.commit
  expires_at           = var.expires_at
  vpc_id               = module.aws_fabric.vpc_id
  internet_gateway_id  = module.aws_fabric.internet_gateway_id
  security_group_id    = module.aws_fabric.security_group_id
  iam_instance_profile = module.aws_fabric.iam_instance_profile
  router_name          = "aws-leaf-a"
  client_name          = "aws-client-a"
  subnet_cidr          = "10.77.60.0/24"
  router_private_ip    = "10.77.60.4"
  client_private_ip    = "10.77.60.11"
  extra_leaf_nodes     = local.aws_extra_leaf_nodes
  ami_id               = var.aws_ami_id
  instance_type        = "t3.large"
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

  providers = {
    oci = oci
  }

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

  # Keep the dependency explicit even though the clone source values also
  # reference the stage. A targeted PVE apply must never race a workload
  # clone against conversion of the qnap copy into a template.
  depends_on = [
    terraform_data.pve_rr_topology,
    proxmox_virtual_environment_vm.pve_shared_template_stage,
  ]

  run_id               = var.run_id
  purpose              = var.purpose
  commit               = var.commit
  expires_at           = var.expires_at
  node_name            = var.pve_node_name
  boot_source          = var.pve_boot_source
  template_vm_id       = local.pve_clone_template_vm_id
  template_source_node = local.pve_clone_source_node
  clone_full           = var.pve_clone_full
  iso_file_id          = var.pve_iso_file_id
  iso_cdrom_interface  = var.pve_iso_cdrom_interface
  cloud_init_interface = var.pve_cloud_init_interface
  datastore_id         = var.pve_datastore_id
  bridge               = var.pve_underlay_bridge
  capture_bridge       = var.pve_capture_bridge
  vlan_id              = var.pve_vlan_id
  router_name          = "pve-leaf-a"
  client_name          = "pve-client-a"
  router_ipv4_cidr     = "10.77.60.34/24"
  client_ipv4_cidr     = "10.77.60.15/24"
  extra_leaf_nodes     = local.pve_extra_leaf_nodes
  ssh_public_key       = var.ssh_public_key
  pve_ssh_host         = var.pve_ssh_host
  username             = var.pve_username
  router_vm_id         = var.pve_router_vm_id
  client_vm_id         = var.pve_client_vm_id
}

module "pve_rr" {
  source = "../../modules/pve_rr"

  depends_on = [
    terraform_data.pve_rr_topology,
    proxmox_virtual_environment_vm.pve_shared_template_stage,
  ]

  run_id               = var.run_id
  purpose              = var.purpose
  commit               = var.commit
  expires_at           = var.expires_at
  rr_nodes             = local.pve_rr_nodes
  boot_source          = var.pve_boot_source
  template_vm_id       = local.pve_clone_template_vm_id
  template_source_node = local.pve_clone_source_node
  clone_full           = var.pve_clone_full
  iso_file_id          = var.pve_iso_file_id
  iso_cdrom_interface  = var.pve_iso_cdrom_interface
  cloud_init_interface = var.pve_cloud_init_interface
  datastore_id         = var.pve_datastore_id
  ssh_public_key       = var.ssh_public_key
  username             = var.pve_username
}
