locals {
  tags = [
    "routerd",
    "sam-e2e",
    lower(var.run_id),
  ]
}

# PVE RRs are deliberately management/underlay-only.  In particular, do not
# add a capture NIC or create a capture bridge here: capture belongs only to
# the PVE leaf topology.
resource "proxmox_virtual_environment_vm" "rr" {
  for_each    = var.rr_nodes
  name        = "routerd-${var.run_id}-${each.key}"
  description = "routerd SAM E2E RR; run=${var.run_id}; purpose=${var.purpose}; commit=${var.commit}; expires=${var.expires_at}"
  node_name   = each.value.pve_host
  vm_id       = each.value.vm_id
  tags        = concat(local.tags, ["rr"])

  # Qualification guests are disposable. A live image may ignore an ACPI
  # shutdown request, so use PVE's immediate stop on destroy instead of
  # holding a release cleanup for the graceful-shutdown timeout.
  stop_on_destroy = true
  timeout_stop_vm = 30

  dynamic "clone" {
    for_each = var.boot_source == "template" ? [1] : []
    content {
      # The seed template lives on the source host but its disks are on
      # shared qnap storage. Keep that source host explicit for cross-host RRs.
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
    bridge  = each.value.underlay_bridge
    vlan_id = each.value.vlan_id
  }

  initialization {
    datastore_id = var.datastore_id
    interface    = var.cloud_init_interface

    ip_config {
      ipv4 {
        # Management DHCP is supplied by the existing PVE underlay.  QGA
        # discovers the assigned address before routerd is configured.
        address = "dhcp"
      }
    }

    user_account {
      username = var.username
      keys     = [var.ssh_public_key]
    }
  }

  agent {
    enabled = true
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
