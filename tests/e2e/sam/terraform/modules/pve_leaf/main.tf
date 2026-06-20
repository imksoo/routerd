locals {
  tags = [
    "routerd",
    "sam-e2e",
    var.run_id,
  ]

  capture_bridge = coalesce(var.capture_bridge, "rsam${substr(md5(var.run_id), 0, 6)}")

  nodes = {
    router = {
      name                 = var.router_name
      vm_name              = "routerd-${var.run_id}-${var.router_name}"
      role                 = "leaf"
      vm_id                = var.router_vm_id
      ipv4_cidr            = var.router_ipv4_cidr
      management_ipv4_cidr = var.router_management_ipv4_cidr
    }
    client = {
      name                 = var.client_name
      vm_name              = "routerd-${var.run_id}-${var.client_name}"
      role                 = "client"
      vm_id                = var.client_vm_id
      ipv4_cidr            = var.client_ipv4_cidr
      management_ipv4_cidr = var.client_management_ipv4_cidr
    }
  }

  extra_router_nodes = {
    for name, node in var.extra_leaf_nodes : name => {
      name                 = name
      vm_name              = "routerd-${var.run_id}-${name}"
      role                 = "leaf"
      vm_id                = node.router_vm_id
      ipv4_cidr            = node.router_ipv4_cidr
      management_ipv4_cidr = node.router_management_ipv4_cidr
    }
  }

  extra_client_nodes = {
    for _, node in var.extra_leaf_nodes : node.client_name => {
      name                 = node.client_name
      vm_name              = "routerd-${var.run_id}-${node.client_name}"
      role                 = "client"
      vm_id                = node.client_vm_id
      ipv4_cidr            = node.client_ipv4_cidr
      management_ipv4_cidr = node.client_management_ipv4_cidr
    }
  }

  all_nodes = merge(local.nodes, local.extra_router_nodes, local.extra_client_nodes)
}

resource "proxmox_virtual_environment_network_linux_bridge" "capture" {
  name      = local.capture_bridge
  node_name = var.node_name
  autostart = true
  comment   = "routerd SAM E2E capture bridge; run=${var.run_id}; expires=${var.expires_at}"
}

resource "proxmox_virtual_environment_vm" "node" {
  for_each    = local.all_nodes
  name        = each.value.vm_name
  description = "routerd SAM E2E ${each.value.role}; run=${var.run_id}; purpose=${var.purpose}; commit=${var.commit}; expires=${var.expires_at}"
  node_name   = var.node_name
  vm_id       = each.value.vm_id
  tags        = concat(local.tags, [each.value.role])

  dynamic "clone" {
    for_each = var.boot_source == "template" ? [1] : []
    content {
      vm_id = var.template_vm_id
      full  = true
    }
  }

  dynamic "cdrom" {
    for_each = var.boot_source == "iso" ? [1] : []
    content {
      file_id   = var.iso_file_id
      interface = var.iso_cdrom_interface
    }
  }

  cpu {
    cores = var.cpu_cores
  }

  memory {
    dedicated = var.memory_mb
  }

  disk {
    datastore_id = var.datastore_id
    interface    = "scsi0"
    size         = var.disk_gb
  }

  network_device {
    bridge  = var.bridge
    vlan_id = var.vlan_id
  }

  network_device {
    bridge = proxmox_virtual_environment_network_linux_bridge.capture.name
  }

  initialization {
    datastore_id = var.datastore_id
    interface    = var.cloud_init_interface

    ip_config {
      ipv4 {
        address = each.value.management_ipv4_cidr
        gateway = var.gateway_ipv4
      }
    }

    ip_config {
      ipv4 {
        address = each.value.ipv4_cidr
        gateway = var.capture_gateway_ipv4
      }
    }

    user_account {
      username = var.username
      keys     = [var.ssh_public_key]
    }
  }

  agent {
    enabled = false
  }

  serial_device {}

  lifecycle {
    precondition {
      condition     = var.boot_source != "template" || var.template_vm_id != null
      error_message = "template_vm_id is required when boot_source is template."
    }
    precondition {
      condition     = var.boot_source != "iso" || var.iso_file_id != null
      error_message = "iso_file_id is required when boot_source is iso."
    }
  }
}
