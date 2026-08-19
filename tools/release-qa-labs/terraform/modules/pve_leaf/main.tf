locals {
  tags = [
    "routerd",
    "sam-e2e",
    lower(var.run_id),
  ]

  capture_bridge = coalesce(var.capture_bridge, "rsam${substr(md5(var.run_id), 0, 6)}")

  nodes = {
    router = {
      name      = var.router_name
      vm_name   = "routerd-${var.run_id}-${var.router_name}"
      role      = "leaf"
      vm_id     = var.router_vm_id
      ipv4_cidr = var.router_ipv4_cidr
    }
    client = {
      name      = var.client_name
      vm_name   = "routerd-${var.run_id}-${var.client_name}"
      role      = "client"
      vm_id     = var.client_vm_id
      ipv4_cidr = var.client_ipv4_cidr
    }
  }

  extra_router_nodes = {
    for name, node in var.extra_leaf_nodes : name => {
      name      = name
      vm_name   = "routerd-${var.run_id}-${name}"
      role      = "leaf"
      vm_id     = node.router_vm_id
      ipv4_cidr = node.router_ipv4_cidr
    }
  }

  extra_client_nodes = {
    for _, node in var.extra_leaf_nodes : node.client_name => {
      name      = node.client_name
      vm_name   = "routerd-${var.run_id}-${node.client_name}"
      role      = "client"
      vm_id     = node.client_vm_id
      ipv4_cidr = node.client_ipv4_cidr
    }
  }

  all_nodes = merge(local.nodes, local.extra_router_nodes, local.extra_client_nodes)
}

resource "proxmox_virtual_environment_vm" "node" {
  for_each    = local.all_nodes
  name        = each.value.vm_name
  description = "routerd SAM E2E ${each.value.role}; run=${var.run_id}; purpose=${var.purpose}; commit=${var.commit}; expires=${var.expires_at}"
  node_name   = var.node_name
  vm_id       = each.value.vm_id
  tags        = concat(local.tags, [each.value.role])

  # Qualification guests are disposable. A live image may ignore an ACPI
  # shutdown request, so use PVE's immediate stop on destroy instead of
  # holding a release cleanup for the graceful-shutdown timeout.
  stop_on_destroy = true
  timeout_stop_vm = 30

  dynamic "clone" {
    for_each = var.boot_source == "template" ? [1] : []
    content {
      # The source is a run-scoped full-copy template on shared storage. Do
      # not infer its node from this leaf's target node: that would make a
      # cross-host deployment look up the source on the wrong PVE host.
      vm_id        = var.template_vm_id
      node_name    = var.template_source_node
      datastore_id = var.datastore_id
      full         = var.clone_full
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
    # The run-scoped, live-only portless capture bridge is staged through the
    # strict root-PVE boundary before this token-scoped VM plan runs.  It is
    # intentionally absent from PVE persistent network configuration.
    bridge = local.capture_bridge
  }

  initialization {
    datastore_id = var.datastore_id
    interface    = var.cloud_init_interface

    ip_config {
      ipv4 {
        # The shared PVE underlay owns management DHCP.  routerd never
        # configures this NIC; the qualification driver obtains the assigned
        # address through QGA before it generates any routerd configuration.
        address = "dhcp"
      }
    }

    # A leaf's capture.sourceAddress is owned by MobilityPool as a /32.
    # Seeding its /24 through cloud-init makes that external subnet look like
    # a managed capture address and leaves a conflicting connected route.
    # Only traffic clients need a static address on the isolated capture L2.
    dynamic "ip_config" {
      for_each = each.value.role == "client" ? [each.value.ipv4_cidr] : []
      content {
        ipv4 {
          address = ip_config.value
          gateway = var.capture_gateway_ipv4
        }
      }
    }

    user_account {
      username = var.username
      keys     = [var.ssh_public_key]
    }
  }

  agent {
    enabled = true
    # Keep the QGA device available for post-apply identity attestation, but
    # do not let the provider block an apply waiting for an address. The
    # bounded attestation step owns readiness and cleanup on failure.
    wait_for_ip {
      disabled = true
    }
  }

  serial_device {}

  lifecycle {
    precondition {
      condition = var.boot_source != "template" || (
        var.template_vm_id != null &&
        var.template_source_node != null &&
        var.clone_full
      )
      error_message = "template boot requires a shared-template VM ID, explicit source node, and full clones."
    }
    precondition {
      condition     = var.boot_source != "iso" || var.iso_file_id != null
      error_message = "iso_file_id is required when boot_source is iso."
    }
  }
}
