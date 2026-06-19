---
title: PVE NoCloud hostname for the live ISO
---

# PVE NoCloud hostname for the live ISO

The routerd live ISO is built from an Ubuntu `debootstrap` root filesystem and
does not install the full `cloud-init` package. For Proxmox VE lab nodes, the
image supports the small part of NoCloud that is needed before routerd starts:
it reads `hostname` from `user-data` on a `cidata`/`CIDATA` config drive and
applies it with `hostnamectl`.

This keeps the live ISO small while still letting multiple VMs boot from the
same ISO and appear as distinct hosts over SSH and in PVE validation logs.

## user-data

Create a PVE snippet with a top-level `hostname` field:

```yaml
#cloud-config
hostname: pve-rt07
```

Attach it as the VM's cloud-init user-data:

```sh
qm set 169 --ide2 local:iso/routerd-live.iso,media=cdrom
qm set 169 --cicustom user=local:snippets/routerd-pve-rt07.yaml
qm set 169 --boot order=ide2
qm reboot 169
```

At boot, the live setup service waits briefly for block devices, searches
NoCloud media labels `CIDATA` and `cidata`, reads `user-data`, validates the
hostname, writes `/etc/hostname`, and calls `hostnamectl set-hostname`.

## Scope

This is intentionally not a full cloud-init implementation. The live ISO only
uses NoCloud for early hostname identity. It does not run cloud-init modules or
apply network, user, package, or SSH-key configuration from user-data.

For richer bootstrap behavior, keep using routerd configuration media or install
Ubuntu Server to disk and manage normal cloud-init there.
