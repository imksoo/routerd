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

Attach it as the VM's cloud-init user-data while the VM is stopped:

```sh
qm set 169 --ide2 local:iso/routerd-live.iso,media=cdrom
qm set 169 --cicustom user=local:snippets/routerd-pve-rt07.yaml
qm set 169 --boot order=ide2
qm start 169
```

At boot, the live setup service:

1. Applies `hostname` from NoCloud user-data.
2. Regenerates SSH host keys so every VM has a distinct host identity.
3. Installs `ssh_authorized_keys` into `/root/.ssh/authorized_keys` and enables
   `ssh.service`.
4. Tries a `ROUTERD_CONFIG` config disk first. It restores `router.yaml` and
   root-only regular files from a sibling `secrets/` directory before routerd
   starts.
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

A persistent `ROUTERD_CONFIG` filesystem uses the same minimal layout:

```text
router.yaml
secrets/
  wg-l2.key
```

Secret files are installed under `/usr/local/etc/routerd/secrets` with mode
`0600`; the directory itself is mode `0700`. Keep the config filesystem
root-only at the hypervisor/storage layer because it contains private keys.

After a successful fetch and checksum verification, the installed `router.yaml`
is cached under `/var/lib/routerd/validated-config/router.yaml`. This cache has
the lifetime of its state filesystem. Ordinary Live ISO COW storage is volatile:
neither this cache nor SQLite history survives a VM reboot. Use persistent
`ROUTERD_CONFIG` media for the authoritative configuration and keys; follow the
[state database policy](../operations/state-database.md) for state and history.

The ISO's default DHCP profile exists only so first boot can reach
`routerd.config_url`. After config restore, the setup service removes that
profile before starting routerd, so routerd's own `DHCPv4Client`,
`IPv4StaticAddress`, and route resources become the network authority.

## Updating a running Live ISO VM

Do not replace the mounted ISO of a running Live guest. Its root filesystem
still reads SquashFS blocks from that medium; changing the medium underneath it
can cause read and decompression errors.

1. Download a version-specific ISO to shared storage and verify its published
   SHA256. Retain the known-good old ISO and the VM's config/cidata media.
2. Check the current configuration and keys against their persistent source.
   Identify any state that cannot be reconstructed and preserve a consistent
   external snapshot where required. Do not overwrite a newly running database
   with an older history snapshot.
3. Keep the old ISO attached while requesting a normal guest poweroff. Observe
   the serial console and hypervisor state until the VM is actually stopped.
   Older media can ask to remove the installation medium and press Enter;
   acknowledge that real prompt rather than treating absent QGA as failure.
4. Only after the VM is stopped, change its ISO and start it. Preserve its
   config disk, cidata, NIC identities, and recovery path. Update redundant
   routers one at a time.
5. Observe actual boot progress, QGA, address acquisition, enrollment, BGP/IPIP,
   and real peer traffic. Judge the configured state and explicit errors, not
   an arbitrary number of elapsed seconds. Reapply the actual configuration
   where needed and verify the resulting runtime.

Current media uses casper's existing `noprompt` kernel option to avoid the
interactive media-removal wait on the next shutdown/reboot. This does not
promise completion within a fixed duration. Keep console access and distinguish
a normal agent restart from a stalled or failed boot; do not reset solely
because an observation command reached its deadline.

## Scope

This is intentionally not a full cloud-init implementation. The live ISO only
uses NoCloud for early hostname identity, root SSH authorized keys, and routerd
config bootstrap. It does not run cloud-init modules or apply network, user, or
package configuration from user-data.

For richer bootstrap behavior, keep using routerd configuration media or install
Ubuntu Server to disk and manage normal cloud-init there.
