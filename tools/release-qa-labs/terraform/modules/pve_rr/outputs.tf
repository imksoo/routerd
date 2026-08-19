output "nodes" {
  value = {
    for name, node in var.rr_nodes : name => {
      name                  = name
      role                  = "rr"
      site                  = "pve"
      ssh_user              = var.username
      pve_host              = node.pve_host
      pve_ssh_host          = node.pve_ssh_host
      vm_id                 = proxmox_virtual_environment_vm.rr[name].vm_id
      management_ip         = null
      private_ip            = null
      public_ip             = null
      pve_management_source = "pending-qga-dhcp"
      underlay_bridge       = node.underlay_bridge
      underlay_vlan_id      = node.vlan_id
    }
  }
}
