# Live ISO validation on PVE

Date: 2026-05-10

## Environment

- PVE node: `pve07`
- Test VM: `999` (`routerd-live-test`)
- ISO: `dist/iso/routerd-live-routerd-live-pvetest6.iso`
- PVE ISO copy: `/var/lib/vz/template/iso/routerd-live-pvetest6.iso`
- VM shape: 2 vCPU, 1536 MiB RAM, serial console socket, no persistent OS disk
- Network:
  - `net0`: `vmbr0` as WAN-side DHCP path
  - `net1`: `vmbr3` as isolated LAN-side test bridge

`vmbr490` was not present on `pve07`, so `vmbr3` was used as the isolated LAN
bridge for this validation.

## Findings

The first validation attempt showed that the VM was running but the serial
console produced no login prompt. Root cause was the BIOS boot path. PVE
SeaBIOS booted Alpine's `boot/syslinux/syslinux.cfg`, not the GRUB path. The
GRUB configuration already had serial settings, but syslinux did not.

Fix: `scripts/build-live-iso.sh` now writes a syslinux configuration with:

- `SERIAL 0 115200`
- `console=tty0 console=ttyS0,115200n8`
- the same routerd live kernel module list as the GRUB path

## Wizard validation

After rebuilding the ISO as `routerd-live-pvetest6`, serial console access via
`qm terminal 999` worked.

Observed flow:

1. Alpine booted on `ttyS0`.
2. Login prompt appeared on the serial console.
3. Login as `root` succeeded.
4. `/root/.profile` printed the routerd live message.
5. The setup wizard started.
6. WAN was configured as DHCP on `eth0`.
7. LAN was configured as `192.168.99.1/24` on `eth1`.
8. DHCPv4, DNS resolver, NTP server, firewall, NAT44, and Web Console were
   enabled.
9. The wizard generated `/usr/local/etc/routerd/router.yaml`.
10. `routerd validate` succeeded.
11. `routerd apply --once` applied the generated configuration.
12. `routerctl status` reported:

```json
{
  "phase": "Healthy",
  "generation": 1,
  "resourceCount": 14
}
```

## USB persistence validation

A temporary 1 GiB PVE disk was attached to the VM and partitioned inside the
live environment:

- disk: `/dev/sda`
- partition: `/dev/sda1`
- filesystem: `ext4`

The following persistence path was validated:

```sh
/usr/share/routerd/live-persistence.sh save-config /dev/sda1 /usr/local/etc/routerd/router.yaml yes 10M
/usr/share/routerd/live-persistence.sh flush
```

The USB persistence directory contained:

```text
/media/routerd-usb/routerd/log-limit
/media/routerd-usb/routerd/logs/<timestamp>.tgz
/media/routerd-usb/routerd/router.yaml
/media/routerd-usb/routerd/state/routerd-varlib.tgz
/media/routerd-usb/routerd/usb-device
/media/routerd-usb/routerd/usb-flush-enabled
```

After reboot, the live system restored:

```text
routerd-live: restored /usr/local/etc/routerd/router.yaml from /dev/sda1
```

`routerd validate --config /usr/local/etc/routerd/router.yaml` succeeded, and
`routerctl status` again reported `phase=Healthy`, `generation=1`,
`resourceCount=14`.

## Cleanup

The test VM was stopped and destroyed with purge enabled:

```sh
qm stop 999 --skiplock 1
qm destroy 999 --purge 1
```

The PVE ISO copy was left in place for later manual testing:

```text
/var/lib/vz/template/iso/routerd-live-pvetest6.iso
```

## Follow-up validation with captured log and screenshots

The validation was repeated after adding the requested serial-console log and
QEMU monitor screenshots.

- PVE node: `pve07`
- Test VM: `999` (`routerd-live-test`)
- Test ISO: `dist/iso/routerd-live-routerd-live-pvefix.iso`
- PVE ISO copy: `/var/lib/vz/template/iso/routerd-live-pvefix.iso`
- Serial log: `/tmp/iso-boot-test-20260510-1742.log`
- Screenshot source: `/tmp/iso-boot-01-grub.png` through
  `/tmp/iso-boot-08-usb-flush.png`
- Screenshot repository copy: `website/static/img/iso-boot/`

The VM was created with both serial and VGA consoles so that `qm terminal` and
QEMU `screendump` could be used in the same run:

```sh
qm create 999 \
  --name routerd-live-test \
  --memory 1536 \
  --cores 2 \
  --ostype l26 \
  --serial0 socket \
  --vga std \
  --boot order=ide2 \
  --ide2 local:iso/routerd-live-pvefix.iso,media=cdrom \
  --net0 virtio,bridge=vmbr0 \
  --net1 virtio,bridge=vmbr3
```

The requested `vmbr490` bridge was not present on `pve07`; `vmbr3` was used as
the isolated LAN-side bridge, matching the earlier successful validation.

The run found one setup wizard bug: after entering `192.168.99.1/24` as the LAN
address, the DHCPv4 pool defaults still showed `192.168.10.100` and
`192.168.10.200`. The installer now derives DHCPv4 pool defaults from the LAN
address prefix. The fixed ISO showed:

```text
DHCPv4 pool start [192.168.99.100]:
DHCPv4 pool end [192.168.99.200]:
```

The fixed run completed with:

```json
{
  "phase": "Healthy",
  "generation": 1,
  "resourceCount": 14
}
```

USB persistence was also rechecked by hot-adding a temporary 1 GiB disk,
creating `/dev/sda1` as `ext4`, saving the generated config, and flushing
state/log archives:

```sh
/usr/share/routerd/live-persistence.sh save-config /dev/sda1 /usr/local/etc/routerd/router.yaml yes 10M
/usr/share/routerd/live-persistence.sh flush
/usr/share/routerd/live-persistence.sh status
```

The helper reported:

```text
routerd-live: USB persistence mounted: /dev/sda1 -> /media/routerd-usb
```

The test VM was stopped and destroyed with purge enabled after the screenshot
and log capture.
