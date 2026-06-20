locals {
  common_tags = {
    ManagedBy       = var.managed_by
    Owner           = var.managed_by
    Project         = "routerd-sam-e2e"
    Environment     = "validation"
    ExpiresAt       = var.expires_at
    routerd-purpose = var.purpose
    routerd-commit  = var.commit
    routerd-run-id  = var.run_id
  }

  nodes = {
    router = {
      name                 = var.router_name
      private_ip           = var.router_private_ip
      role                 = "leaf"
      enable_ip_forwarding = true
      custom_data          = base64encode(local.router_cloud_init)
    }
    client = {
      name                 = var.client_name
      private_ip           = var.client_private_ip
      role                 = "client"
      enable_ip_forwarding = false
      custom_data          = null
    }
  }

  extra_router_nodes = {
    for name, node in var.extra_leaf_nodes : name => {
      name                 = name
      private_ip           = node.router_private_ip
      role                 = "leaf"
      enable_ip_forwarding = true
      custom_data          = base64encode(local.router_cloud_init)
    }
  }

  extra_client_nodes = {
    for _, node in var.extra_leaf_nodes : node.client_name => {
      name                 = node.client_name
      private_ip           = node.client_private_ip
      role                 = "client"
      enable_ip_forwarding = false
      custom_data          = null
    }
  }

  all_nodes    = merge(local.nodes, local.extra_router_nodes, local.extra_client_nodes)
  router_nodes = { for key, node in local.all_nodes : key => node if node.role == "leaf" }

  router_cloud_init = <<-CLOUD_INIT
    #cloud-config
    runcmd:
      - [bash, -lc, "curl -fsSL 'https://azurecliprod.blob.core.windows.net/$root/deb_install.sh' -o /tmp/InstallAzureCLIDeb.sh"]
      - [bash, -lc, 'bash /tmp/InstallAzureCLIDeb.sh']
      - [bash, -lc, 'az version --output json']
      - [bash, -lc, 'rm -f /tmp/InstallAzureCLIDeb.sh']
  CLOUD_INIT
}

resource "azurerm_resource_group" "lab" {
  name     = "rg-routerd-${var.run_id}-azure"
  location = var.location
  tags     = local.common_tags
}

resource "azurerm_virtual_network" "lab" {
  name                = "vnet-routerd-${var.run_id}-azure"
  location            = azurerm_resource_group.lab.location
  resource_group_name = azurerm_resource_group.lab.name
  address_space       = [var.address_space]
  tags                = local.common_tags
}

resource "azurerm_subnet" "leaf" {
  name                 = "snet-routerd-${var.run_id}-azure-leaf"
  resource_group_name  = azurerm_resource_group.lab.name
  virtual_network_name = azurerm_virtual_network.lab.name
  address_prefixes     = [var.subnet_cidr]
}

resource "azurerm_network_security_group" "lab" {
  name                = "nsg-routerd-${var.run_id}-azure"
  location            = azurerm_resource_group.lab.location
  resource_group_name = azurerm_resource_group.lab.name
  tags                = local.common_tags

  dynamic "security_rule" {
    for_each = toset(var.ssh_cidr_blocks)
    content {
      name                       = "allow-ssh-${replace(replace(security_rule.value, "/", "-"), ".", "-")}"
      priority                   = 100 + index(var.ssh_cidr_blocks, security_rule.value)
      direction                  = "Inbound"
      access                     = "Allow"
      protocol                   = "Tcp"
      source_port_range          = "*"
      destination_port_range     = "22"
      source_address_prefix      = security_rule.value
      destination_address_prefix = "*"
    }
  }

  security_rule {
    name                       = "allow-wireguard"
    priority                   = 200
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Udp"
    source_port_range          = "*"
    destination_port_range     = "51820"
    source_address_prefix      = "*"
    destination_address_prefix = "*"
  }

  security_rule {
    name                       = "allow-icmp"
    priority                   = 210
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Icmp"
    source_port_range          = "*"
    destination_port_range     = "*"
    source_address_prefix      = "*"
    destination_address_prefix = "*"
  }

  security_rule {
    name                       = "allow-leaf-subnet"
    priority                   = 220
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "*"
    source_port_range          = "*"
    destination_port_range     = "*"
    source_address_prefix      = var.subnet_cidr
    destination_address_prefix = var.subnet_cidr
  }
}

resource "azurerm_subnet_network_security_group_association" "leaf" {
  subnet_id                 = azurerm_subnet.leaf.id
  network_security_group_id = azurerm_network_security_group.lab.id
}

resource "azurerm_route_table" "leaf" {
  name                = "rt-routerd-${var.run_id}-azure-leaf"
  location            = azurerm_resource_group.lab.location
  resource_group_name = azurerm_resource_group.lab.name
  tags                = local.common_tags
}

resource "azurerm_subnet_route_table_association" "leaf" {
  subnet_id      = azurerm_subnet.leaf.id
  route_table_id = azurerm_route_table.leaf.id
}

resource "azurerm_public_ip" "node" {
  for_each            = local.all_nodes
  name                = "${each.value.name}PublicIP"
  location            = azurerm_resource_group.lab.location
  resource_group_name = azurerm_resource_group.lab.name
  allocation_method   = "Static"
  sku                 = "Standard"
  tags                = merge(local.common_tags, { routerd-role = each.value.role })
}

resource "azurerm_network_interface" "node" {
  for_each                       = local.all_nodes
  name                           = "${each.value.name}VMNic"
  location                       = azurerm_resource_group.lab.location
  resource_group_name            = azurerm_resource_group.lab.name
  ip_forwarding_enabled          = each.value.enable_ip_forwarding
  accelerated_networking_enabled = false
  tags = merge(local.common_tags, {
    routerd-role       = each.value.role
    cloudedge-mobility = each.key == "client" ? "true" : "false"
  })

  ip_configuration {
    name                          = "ipconfig-${each.value.name}"
    subnet_id                     = azurerm_subnet.leaf.id
    private_ip_address_allocation = "Static"
    private_ip_address            = each.value.private_ip
    public_ip_address_id          = azurerm_public_ip.node[each.key].id
    primary                       = true
  }
}

resource "azurerm_linux_virtual_machine" "node" {
  for_each                        = local.all_nodes
  name                            = each.value.name
  location                        = azurerm_resource_group.lab.location
  resource_group_name             = azurerm_resource_group.lab.name
  size                            = var.vm_size
  admin_username                  = var.admin_username
  network_interface_ids           = [azurerm_network_interface.node[each.key].id]
  disable_password_authentication = true
  custom_data                     = each.value.custom_data
  tags = merge(local.common_tags, {
    routerd-role       = each.value.role
    cloudedge-mobility = each.key == "client" ? "true" : "false"
  })

  admin_ssh_key {
    username   = var.admin_username
    public_key = var.ssh_public_key
  }

  identity { type = "SystemAssigned" }

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Standard_LRS"
  }

  source_image_reference {
    publisher = "Canonical"
    offer     = "0001-com-ubuntu-server-jammy"
    sku       = "22_04-lts-gen2"
    version   = "latest"
  }
}

resource "azurerm_role_assignment" "router_rg_contributor" {
  for_each                         = local.router_nodes
  scope                            = azurerm_resource_group.lab.id
  role_definition_name             = "Contributor"
  principal_id                     = azurerm_linux_virtual_machine.node[each.key].identity[0].principal_id
  skip_service_principal_aad_check = true
}
