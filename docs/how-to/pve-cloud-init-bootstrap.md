# PVE cloud-init bootstrap

routerd live ISO can boot on Proxmox VE with a small NoCloud `user-data`
snippet. The snippet points the VM at the real `router.yaml`; the live image
fetches it after DHCP and starts routerd normally.

This keeps the ISO and VM template shared across PVE routers. Per-node data can
stay in Proxmox cloud-init snippets while the full routerd config lives on an
HTTP/object endpoint.

## user-data

Create a Proxmox snippet such as `/var/lib/vz/snippets/routerd-pve-rt07.yaml`:

```yaml
#cloud-config
hostname: pve-rt07
routerd:
  config_url: http://10.0.0.10/routerd/pve-rt07/router.yaml
  config_sha256: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

`config_sha256` is optional, but recommended. The live image refuses to install
the downloaded config when the hash does not match.

The parser accepts these aliases:

- `config_url`, `config-url`, `configUrl`, `routerd_config_url`, `routerd-config-url`
- `config_sha256`, `config-sha256`, `configSha256`, `routerd_config_sha256`, `routerd-config-sha256`

## PVE VM wiring

Attach the live ISO and the cloud-init snippet:

```sh
qm set 169 --ide2 qnap:iso/routerd-live.iso,media=cdrom
qm set 169 --cicustom user=local:snippets/routerd-pve-rt07.yaml
qm set 169 --boot order=ide2
qm reboot 169
```

The boot sequence is:

1. Try the existing `ROUTERD_CONFIG` config disk paths first.
2. If no local `router.yaml` was restored, start DHCP.
3. Search NoCloud media labels `CIDATA`, `cidata`, `CONFIG-2`, and `config-2`.
4. Read `user-data`, fetch `routerd.config_url`, verify `config_sha256` when set.
5. Install the downloaded file as `/usr/local/etc/routerd/router.yaml`.
6. Start `routerd-bgp` and `routerd serve`.

The existing config disk layout continues to work. Cloud-init is only used when
no local config has already been restored.
