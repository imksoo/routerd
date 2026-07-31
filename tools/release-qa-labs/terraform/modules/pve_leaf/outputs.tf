output "capture_bridge" {
  value = proxmox_virtual_environment_network_linux_bridge.capture.name
}

output "router" {
  value = {
    name           = var.router_name
    role           = "leaf"
    site           = "pve"
    ssh_user       = var.username
    vm_id          = proxmox_virtual_environment_vm.node["router"].vm_id
    private_ip     = split("/", var.router_ipv4_cidr)[0]
    public_ip      = split("/", var.router_management_ipv4_cidr)[0]
    capture_bridge = proxmox_virtual_environment_network_linux_bridge.capture.name
  }
}

output "client" {
  value = {
    name           = var.client_name
    role           = "client"
    site           = "pve"
    ssh_user       = var.username
    vm_id          = proxmox_virtual_environment_vm.node["client"].vm_id
    private_ip     = split("/", var.client_ipv4_cidr)[0]
    public_ip      = split("/", var.client_management_ipv4_cidr)[0]
    capture_bridge = proxmox_virtual_environment_network_linux_bridge.capture.name
  }
}

output "routers" {
  value = {
    for key, node in local.all_nodes : node.name => {
      name           = node.name
      role           = "leaf"
      site           = "pve"
      ssh_user       = var.username
      vm_id          = proxmox_virtual_environment_vm.node[key].vm_id
      private_ip     = split("/", node.ipv4_cidr)[0]
      public_ip      = split("/", node.management_ipv4_cidr)[0]
      capture_bridge = proxmox_virtual_environment_network_linux_bridge.capture.name
    } if node.role == "leaf"
  }
}

output "clients" {
  value = {
    for key, node in local.all_nodes : node.name => {
      name           = node.name
      role           = "client"
      site           = "pve"
      ssh_user       = var.username
      vm_id          = proxmox_virtual_environment_vm.node[key].vm_id
      private_ip     = split("/", node.ipv4_cidr)[0]
      public_ip      = split("/", node.management_ipv4_cidr)[0]
      capture_bridge = proxmox_virtual_environment_network_linux_bridge.capture.name
    } if node.role == "client"
  }
}
