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
4. Tries a `ROUTERD_CONFIG` config disk first. It restores `router.yaml` and
   root-only regular files from a sibling `secrets/` directory before routerd
   starts.
5. If no config disk is present, fetches `routerd.config_url` with `curl`.
6. Verifies `routerd.config_sha256` when present.
7. Installs the fetched `router.yaml` or extracts a supported config bundle.
8. Falls back to the last validated cache, then to the built-in sample config
   when no external config is available.
9. Validates the restored configuration and prepares `routerd.service`. Only
   after both succeed does it remove the bootstrap systemd-networkd DHCP profile
   and allow `routerd.service` to start. Failure retains bootstrap management.

Supported bundle URLs currently end in `.tar.zst`, `.tzst`, `.tar.gz`, `.tgz`,
or `.tar`. Bundles must contain `router.yaml` at the archive root. Optional
`secrets/` and `metadata.json` entries are installed under
`/etc/routerd/`.

A persistent `ROUTERD_CONFIG` filesystem uses the same minimal layout:

```text
router.yaml
secrets/
  wg-l2.key
```

Secret files are installed under `/etc/routerd/secrets` with mode
`0600`; the directory itself is mode `0700`. Keep the config filesystem
root-only at the hypervisor/storage layer because it contains private keys.

After a successful fetch, checksum verification and configuration validation,
the installed `router.yaml` is cached under
`/var/lib/routerd/validated-config/router.yaml`. Invalid input cannot overwrite
that cache. If a later boot
cannot fetch `routerd.config_url`, the live ISO restores that validated cache.
The cached configuration is validated again before installation.

The ISO's default DHCP profile exists only so first boot can reach
`routerd.config_url`. After config restore, the setup service removes that
profile before starting routerd, so routerd's own `DHCPv4Client`,
`IPv4StaticAddress`, and route resources become the network authority.

## Building the image

`make live-iso` runs the builder as root inside a private mount namespace using
`unshare` (and `sudo` when needed). The builder requires mount namespace support;
there is no fallback into the caller's namespace. Package ownership and
setuid/setgid modes are preserved. The chroot gets a private `/run`, without the
host's service-manager sockets.

One build holds the work-directory lock. Only a work directory carrying this
builder's exact ownership marker can be removed on reuse, and a mounted path
below it causes refusal before recursive traversal. Failed unmounts remain
errors; the builder neither lazily detaches them nor walks through them for
cleanup. Use a new work directory when an older build has no ownership marker.

Rootfs caches now use format `routerd-live-rootfs-v3`. Both archive creation and
extraction preserve numeric ownership, package modes, `user.*` extended attributes,
and Linux `security.capability`. Other security/system attribute namespaces are
not imported from the build machine. GNU tar's `--xattrs` alone restores only the
user namespace; the builder explicitly includes file capabilities as well.
Rebuild v1/v2 caches: earlier ownership changes or omitted file capabilities cannot
be repaired reliably from those archives. The release and quality workflows use
the new exact cache key. Local shell tests cover refusal, attribute round trips,
and firstboot ordering; native ISO building and VM boot remain separate
qualification steps.

## Scope

This is intentionally not a full cloud-init implementation. The live ISO only
uses NoCloud for early hostname identity, root SSH authorized keys, and routerd
config bootstrap. It does not run cloud-init modules or apply network, user, or
package configuration from user-data.

For richer bootstrap behavior, keep using routerd configuration media or install
Ubuntu Server to disk and manage normal cloud-init there.

The cache regression checks `security.capability` when explicitly run as root;
ordinary unprivileged runs cover `user.*` only. From the repository root:

```sh
cache_test_dir=$(mktemp -d /tmp/routerd-live-cache.XXXXXX)
go test -c -o "$cache_test_dir/cache.test" ./tests/liveiso
(cd tests/liveiso && sudo "$cache_test_dir/cache.test" -test.run '^TestLiveRootfsCacheStoresPortableNumericOwnership$' -test.v -test.timeout 30s)
rm -r -- "$cache_test_dir"
```

Require the log stating that the actual cache writer/reader preserved
`security.capability`. The fixture is inert data and is never executed; this
checks native file metadata without changing the host network.

## Independent-client boot check

Use the canonical `make live-iso` image in a dedicated disposable VM, with a
read-only `ROUTERD_CONFIG` drive containing
`tests/fixtures/linux-live-iso-boot.json` as `router.yaml`. This prepared 1.5
configuration uses three physical guest ports: `ens18` for LAN, `ens19` for
WAN and `ens20` for management. Check the hypervisor MAC-to-port mapping before
judging the product result; rewrite these three names for a different guest
layout and run `routerd validate --config router.yaml` before boot. Keep these
ports on isolated lab segments. Control the trial through the hypervisor guest
agent or serial console, independently of the tested IP paths.

The independent peer is another Linux VM. On a trunk carrying the corresponding
LAN/WAN/management segments as VLANs 3131/3132/3133, use
`scripts/linux-router-reboot-fixture.sh --disposable-vm setup-peer TRUNK_IF`.
This IPv4 peer mode supplies the upstream DNS/HTTP servers, management positive
control and a BusyBox DHCP client. Start it before the ISO router boots.

Within 120 seconds after guest-agent availability, require successful
`routerd-live-setup.service`, active `routerd.service`, and Healthy control
status with no controller errors. Confirm that the canonical configuration
matches the config-drive input, bootstrap DHCP has been withdrawn, and the
image commit marker and installed executables match the built image. No
runtime binary replacement or manual product-state repair is allowed.
Run the peer's `probe` and `isolation` modes: DHCP acquisition, local/upstream
DNS A over UDP/TCP, peer-observed NAT source and management-to-LAN access must
work; WAN-to-LAN, LAN-to-management and WAN-to-router DNS must be denied.
For default-route transit, add `198.18.18.1/32` to the WAN peer namespace loopback
and require a LAN HTTP request to `http://198.18.18.1:8080/routed-default` to
return the NAT source `198.18.16.2`. This destination lies beyond the connected
WAN subnet; a request to the connected WAN address alone cannot prove the
default route.
Require a natural renewal by the same DHCP process and address, plus stable
routerd PID/restart count during the observation. Check native firewall deny
counters rather than relying on a client timeout alone.

Also verify distribution metadata: package executors and plugin manifests must
exist at their canonical paths, temporary directories must have mode 1777,
setuid package files must retain their package owner/mode, and the image's ping
must retain `security.capability`. If a rootfs cache is used, compare numeric
owner, mode and capability bytes before and after an actual cache restore.
A cache format marker alone is not proof that attributes survived.

This check qualifies the image's clean boot and the stated IPv4 router path.
It does not cover IPv6, physical ISP behavior, persistent disk installation,
power-loss recovery or complete feature/platform parity. On failure, preserve
only the bounded output needed to identify the failed boot/configuration or
packet path, reproduce product failures locally before another lab run, and
stop that qualification attempt. On success, withdraw with an empty 1.5 config,
run peer cleanup, and remove both disposable VMs, their disks, ISO/config media,
rootfs cache and build staging. Keep this procedure and scoped result instead
of the successful raw logs.


Scoped result (2026-11-03): ISO source `0ee9777e`, corrected boot fixture
`5e5e9a76`, Ubuntu 24.04 / Linux 6.8.0-147, passed this two-VM clean-boot and
IPv4 procedure, including natural renewal, default-route transit, exact payload
hashes, native package metadata and empty-intent withdrawal. The ISO SHA-256 is
`fba24c3b714c8779e608de047d952ad45735fb0a302ade9252e6cc1d62b6a569`.
The initial wrong raw device name was a fixture preparation failure; the final
result used a corrected config drive and cold boot of the unchanged ISO.
VMs, disks and temporary media were removed after the successful check.
