output "resource_group_name" { value = azurerm_resource_group.lab.name }
output "vnet_name" { value = azurerm_virtual_network.lab.name }
output "subnet_id" { value = azurerm_subnet.leaf.id }
output "subnet_name" { value = azurerm_subnet.leaf.name }
output "route_table_name" { value = azurerm_route_table.leaf.name }

output "router" {
  value = {
    name         = var.router_name
    role         = "leaf"
    site         = "azure"
    ssh_user     = var.admin_username
    instance_id  = azurerm_linux_virtual_machine.node["router"].id
    interface_id = azurerm_network_interface.node["router"].id
    private_ip   = local.nodes.router.private_ip
    public_ip    = azurerm_public_ip.node["router"].ip_address
    principal_id = azurerm_linux_virtual_machine.node["router"].identity[0].principal_id
  }
}

output "client" {
  value = {
    name         = var.client_name
    role         = "client"
    site         = "azure"
    ssh_user     = var.admin_username
    instance_id  = azurerm_linux_virtual_machine.node["client"].id
    interface_id = azurerm_network_interface.node["client"].id
    private_ip   = local.nodes.client.private_ip
    public_ip    = azurerm_public_ip.node["client"].ip_address
  }
}

output "routers" {
  value = {
    for key, node in local.all_nodes : node.name => {
      name         = node.name
      role         = "leaf"
      site         = "azure"
      ssh_user     = var.admin_username
      instance_id  = azurerm_linux_virtual_machine.node[key].id
      interface_id = azurerm_network_interface.node[key].id
      private_ip   = node.private_ip
      public_ip    = azurerm_public_ip.node[key].ip_address
      principal_id = azurerm_linux_virtual_machine.node[key].identity[0].principal_id
    } if node.role == "leaf"
  }
}

output "clients" {
  value = {
    for key, node in local.all_nodes : node.name => {
      name         = node.name
      role         = "client"
      site         = "azure"
      ssh_user     = var.admin_username
      instance_id  = azurerm_linux_virtual_machine.node[key].id
      interface_id = azurerm_network_interface.node[key].id
      private_ip   = node.private_ip
      public_ip    = azurerm_public_ip.node[key].ip_address
    } if node.role == "client"
  }
}
