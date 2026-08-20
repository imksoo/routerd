locals {
  pve_router_nodes = merge(
    module.pve_rr.nodes,
    module.pve_leaf.routers,
  )

  pve_client_nodes = module.pve_leaf.clients

  # PVE is certified before any cloud resource exists.  Keep its output
  # independent from cloud modules so the PVE phase never refreshes cloud
  # providers merely to discover its own QGA addresses.
  pve_nodes = merge(local.pve_router_nodes, local.pve_client_nodes)

  overlay_ips = {
    pve-rr-a     = "10.99.0.1"
    pve-rr-b     = "10.99.0.6"
    aws-leaf-a   = "10.99.0.2"
    aws-leaf-b   = "10.99.0.7"
    azure-leaf-a = "10.99.0.3"
    azure-leaf-b = "10.99.0.8"
    oci-leaf-a   = "10.99.0.4"
    oci-leaf-b   = "10.99.0.9"
    pve-leaf-a   = "10.99.0.5"
    pve-leaf-b   = "10.99.0.10"
  }

  router_nodes = merge(
    { for name, node in local.pve_router_nodes : name => merge(node, { overlay_ip = local.overlay_ips[name] }) },
    { for name, node in module.aws_leaf.routers : name => merge(node, { overlay_ip = local.overlay_ips[name] }) },
    { for name, node in module.azure_leaf.routers : name => merge(node, { overlay_ip = local.overlay_ips[name] }) },
    { for name, node in module.oci_leaf.routers : name => merge(node, { overlay_ip = local.overlay_ips[name] }) }
  )

  client_nodes = merge(
    { for name, node in module.aws_leaf.clients : name => merge(node, { client_ip = node.private_ip }) },
    { for name, node in module.azure_leaf.clients : name => merge(node, { client_ip = node.private_ip }) },
    { for name, node in module.oci_leaf.clients : name => merge(node, { client_ip = node.private_ip }) },
    { for name, node in local.pve_client_nodes : name => merge(node, { client_ip = node.private_ip }) }
  )

  pve_fabric = {
    leaf_host     = var.pve_node_name
    leaf_ssh_host = var.pve_ssh_host
    boot_source   = var.pve_boot_source
    template_stage = var.pve_boot_source == "template" ? {
      vm_id       = proxmox_virtual_environment_vm.pve_shared_template_stage[0].vm_id
      source_node = proxmox_virtual_environment_vm.pve_shared_template_stage[0].node_name
      datastore   = var.pve_datastore_id
    } : null
    leaf_underlay       = var.pve_underlay_bridge
    leaf_capture_bridge = module.pve_leaf.capture_bridge
    rr_fault_domain     = var.pve_rr_fault_domain
    rr_nodes            = keys(module.pve_rr.nodes)
    rr_hosts            = sort(distinct([for node in values(local.pve_rr_nodes) : node.pve_host]))
    rr_ssh_hosts        = sort(distinct([for node in values(local.pve_rr_nodes) : node.pve_ssh_host]))
    rr_underlay_bridges = {
      for name, node in module.pve_rr.nodes : name => node.underlay_bridge
    }
    leaf_nodes   = keys(module.pve_leaf.routers)
    client_nodes = keys(module.pve_leaf.clients)
  }
}

output "pve_nodes" {
  value = local.pve_nodes
}

output "pve_fabric" {
  value = local.pve_fabric
}

output "nodes" {
  value = merge(local.router_nodes, local.client_nodes)
}

output "fabric" {
  value = {
    run_id              = var.run_id
    topology_scale      = var.topology_scale
    mobility_prefix     = "10.77.60.0/24"
    tunnel_inner_prefix = "10.255.0.0/20"
    wg_port             = 51820
    bgp_asn             = 64577
    aws = {
      region               = var.aws_region
      vpc_id               = module.aws_fabric.vpc_id
      internet_gateway_id  = module.aws_fabric.internet_gateway_id
      security_group_id    = module.aws_fabric.security_group_id
      iam_instance_profile = module.aws_fabric.iam_instance_profile
      leaf_subnet_id       = module.aws_leaf.subnet_id
      leaf_route_table_id  = module.aws_leaf.route_table_id
      leaf_nodes           = keys(module.aws_leaf.routers)
      client_nodes         = keys(module.aws_leaf.clients)
    }
    azure = {
      location            = var.azure_location
      resource_group_name = module.azure_leaf.resource_group_name
      subnet_id           = module.azure_leaf.subnet_id
      subnet_name         = module.azure_leaf.subnet_name
      route_table_name    = module.azure_leaf.route_table_name
      leaf_nodes          = keys(module.azure_leaf.routers)
      client_nodes        = keys(module.azure_leaf.clients)
    }
    oci = {
      region         = var.oci_region
      compartment_id = var.oci_compartment_id
      vcn_id         = module.oci_leaf.vcn_id
      subnet_id      = module.oci_leaf.subnet_id
      route_table_id = module.oci_leaf.route_table_id
      leaf_nodes     = keys(module.oci_leaf.routers)
      client_nodes   = keys(module.oci_leaf.clients)
    }
    pve = local.pve_fabric
  }
}
