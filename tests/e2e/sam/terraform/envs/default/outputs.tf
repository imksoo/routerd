locals {
  overlay_ips = {
    aws-rr-a     = "10.99.0.1"
    aws-rr-b     = "10.99.0.6"
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
    { for name, node in module.aws_rr.nodes : name => merge(node, { overlay_ip = local.overlay_ips[name] }) },
    { for name, node in module.aws_leaf.routers : name => merge(node, { overlay_ip = local.overlay_ips[name] }) },
    { for name, node in module.azure_leaf.routers : name => merge(node, { overlay_ip = local.overlay_ips[name] }) },
    { for name, node in module.oci_leaf.routers : name => merge(node, { overlay_ip = local.overlay_ips[name] }) },
    { for name, node in module.pve_leaf.routers : name => merge(node, { overlay_ip = local.overlay_ips[name] }) }
  )

  client_nodes = merge(
    { for name, node in module.aws_leaf.clients : name => merge(node, { client_ip = node.private_ip }) },
    { for name, node in module.azure_leaf.clients : name => merge(node, { client_ip = node.private_ip }) },
    { for name, node in module.oci_leaf.clients : name => merge(node, { client_ip = node.private_ip }) },
    { for name, node in module.pve_leaf.clients : name => merge(node, { client_ip = node.private_ip }) }
  )
}

output "nodes" {
  value = merge(local.router_nodes, local.client_nodes)
}

output "fabric" {
  value = {
    run_id              = var.run_id
    topology_scale      = var.topology_scale
    mobility_prefix     = "10.77.60.0/24"
    tunnel_inner_prefix = "10.255.0.0/24"
    wg_port             = 51820
    bgp_asn             = 64577
    aws = {
      region              = var.aws_region
      vpc_id              = module.aws_rr.vpc_id
      rr_route_table      = module.aws_rr.route_table_id
      rr_nodes            = keys(module.aws_rr.nodes)
      leaf_subnet_id      = module.aws_leaf.subnet_id
      leaf_route_table_id = module.aws_leaf.route_table_id
      leaf_nodes          = keys(module.aws_leaf.routers)
      client_nodes        = keys(module.aws_leaf.clients)
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
    pve = {
      node_name       = var.pve_node_name
      boot_source     = var.pve_boot_source
      underlay_bridge = var.pve_underlay_bridge
      capture_bridge  = module.pve_leaf.capture_bridge
      leaf_nodes      = keys(module.pve_leaf.routers)
      client_nodes    = keys(module.pve_leaf.clients)
    }
  }
}
