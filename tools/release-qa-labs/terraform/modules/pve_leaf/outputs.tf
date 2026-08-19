output "capture_bridge" {
  value = local.capture_bridge
}

output "router" {
  value = {
    name                  = var.router_name
    role                  = "leaf"
    site                  = "pve"
    ssh_user              = var.username
    pve_host              = var.node_name
    pve_ssh_host          = var.pve_ssh_host
    vm_id                 = proxmox_virtual_environment_vm.node["router"].vm_id
    private_ip            = split("/", var.router_ipv4_cidr)[0]
    management_ip         = null
    public_ip             = null
    pve_management_source = "pending-qga-dhcp"
    capture_bridge        = local.capture_bridge
  }
}

output "client" {
  value = {
    name                  = var.client_name
    role                  = "client"
    site                  = "pve"
    ssh_user              = var.username
    pve_host              = var.node_name
    pve_ssh_host          = var.pve_ssh_host
    vm_id                 = proxmox_virtual_environment_vm.node["client"].vm_id
    private_ip            = split("/", var.client_ipv4_cidr)[0]
    management_ip         = null
    public_ip             = null
    pve_management_source = "pending-qga-dhcp"
    capture_bridge        = local.capture_bridge
  }
}

output "routers" {
  value = {
    for key, node in local.all_nodes : node.name => {
      name                  = node.name
      role                  = "leaf"
      site                  = "pve"
      ssh_user              = var.username
      pve_host              = var.node_name
      pve_ssh_host          = var.pve_ssh_host
      vm_id                 = proxmox_virtual_environment_vm.node[key].vm_id
      private_ip            = split("/", node.ipv4_cidr)[0]
      management_ip         = null
      public_ip             = null
      pve_management_source = "pending-qga-dhcp"
      capture_bridge        = local.capture_bridge
    } if node.role == "leaf"
  }
}

output "clients" {
  value = {
    for key, node in local.all_nodes : node.name => {
      name                  = node.name
      role                  = "client"
      site                  = "pve"
      ssh_user              = var.username
      pve_host              = var.node_name
      pve_ssh_host          = var.pve_ssh_host
      vm_id                 = proxmox_virtual_environment_vm.node[key].vm_id
      private_ip            = split("/", node.ipv4_cidr)[0]
      management_ip         = null
      public_ip             = null
      pve_management_source = "pending-qga-dhcp"
      capture_bridge        = local.capture_bridge
    } if node.role == "client"
  }
}
