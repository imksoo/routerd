---
title: PVE NoCloud bootstrap for the live ISO
---

# PVE NoCloud bootstrap for the live ISO

The routerd live ISO is built from an Ubuntu `debootstrap` root filesystem and
does not install the full `cloud-init` package. For Proxmox VE lab nodes, the
image supports the small part of NoCloud that is needed before routerd starts:
it reads `hostname`, `routerd.config_url`, and `routerd.config_sha256` from
`user-data` on a `cidata`/`CIDATA` config drive.

This keeps the live ISO small while still letting multiple VMs boot from the
same ISO, appear as distinct hosts over SSH and in PVE validation logs, and pull
their full routerd config from HTTP or object storage.

## user-data

Create a PVE snippet with a top-level `hostname` field and an optional routerd
config pointer:

```yaml
#cloud-config
hostname: pve-rt07
routerd:
  config_url: http://10.0.0.10/routerd/pve-rt07/router.yaml
  config_sha256: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
ssh_authorized_keys:
  - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... admin@example
```

Attach it as the VM's cloud-init user-data:

```sh
qm set 169 --ide2 local:iso/routerd-live.iso,media=cdrom
qm set 169 --cicustom user=local:snippets/routerd-pve-rt07.yaml
qm set 169 --boot order=ide2
qm reboot 169
```

At boot, the live setup service:

1. Applies `hostname` from NoCloud user-data.
2. Regenerates SSH host keys so every VM has a distinct host identity.
3. Installs `ssh_authorized_keys` into `/root/.ssh/authorized_keys` and enables
   `ssh.service`.
4. Tries a `ROUTERD_CONFIG` config disk first.
5. If no config disk is present, fetches `routerd.config_url` with `curl`.
6. Verifies `routerd.config_sha256` when present.
7. Installs the fetched `router.yaml` or extracts a supported config bundle.
8. Falls back to the last validated cache, then to the built-in sample config
   when no external config is available.
9. Removes the bootstrap systemd-networkd DHCP profile and starts
   `routerd.service`.

Supported bundle URLs currently end in `.tar.zst`, `.tzst`, `.tar.gz`, `.tgz`,
or `.tar`. Bundles must contain `router.yaml` at the archive root. Optional
`secrets/` and `metadata.json` entries are installed under
`/usr/local/etc/routerd/`.

After a successful fetch and checksum verification, the installed `router.yaml`
is cached under `/var/lib/routerd/validated-config/router.yaml`. If a later boot
cannot fetch `routerd.config_url`, the live ISO restores that validated cache.

The ISO's default DHCP profile exists only so first boot can reach
`routerd.config_url`. After config restore, the setup service removes that
profile before starting routerd, so routerd's own `DHCPv4Client`,
`IPv4StaticAddress`, and route resources become the network authority.

## Scope

This is intentionally not a full cloud-init implementation. The live ISO only
uses NoCloud for early hostname identity, root SSH authorized keys, and routerd
config bootstrap. It does not run cloud-init modules or apply network, user, or
package configuration from user-data.

For richer bootstrap behavior, keep using routerd configuration media or install
Ubuntu Server to disk and manage normal cloud-init there.
