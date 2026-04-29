---
title: Host inventory
slug: /operations/inventory
---

# Host inventory

routerd collects a small inventory of host facts at the start of every
apply pass and stores it as `routerd.net/v1alpha1/Inventory/host`. The
goal is to give apply, render, and operators a reliable observed-fact
set so decisions don't have to guess.

## What's collected

| Field | Source |
|---|---|
| OS name and version | runtime + `/etc/os-release` |
| Kernel name and version | `uname` |
| Virtualization | `systemd-detect-virt` (Linux), `kern.vm_guest` (FreeBSD) |
| DMI vendor (best effort) | `/sys/class/dmi/id/sys_vendor` |
| Service manager | systemd / rc.d detection |
| Available commands | `nft`, `pf`, `dnsmasq`, `dhcp6c`, `sysctl`, ... |

Inventory is **observation only**. It is not declared in the YAML.

## Inspecting

```bash
routerctl describe inventory/host
routerctl show inventory/host -o yaml
```

`routerctl describe` shows a multi-line summary. `routerctl show` emits
the full structured payload, which is the same JSON that lives in the
SQLite `objects` row.

## What it's used for today

- Operators can read it to confirm the host they think they're running
  on (especially after migrating between Ubuntu and NixOS, or moving a
  VM between Proxmox and bare metal).
- Apply records an `InventoryObserved` event whenever the inventory
  changes between passes. That gives a paper trail when, for example,
  the kernel was upgraded.

## What it will be used for

The renderer will start branching on inventory in future versions:

- Skip the systemd-networkd path on hosts where service manager is
  `rc.d`.
- Suggest `multicast_snooping=0` on virtual hosts.
- Fail early if a required command is missing (e.g. `dnsmasq` not
  installed).

The current implementation just records inventory; it does not consume
it inside the renderer yet. That is intentional — first establish the
observed-fact base, then add branches that refer to it.

## Privacy considerations

Inventory is local to the routerd state database. It is not transmitted
anywhere. If you back up the database, the inventory rows go with it.
DMI vendor and OS version are usually identifying enough for a fleet,
so treat the database with the same care you would `/etc/os-release`.
