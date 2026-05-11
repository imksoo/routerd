# Phase 3.4 A4: live ISO USB persistence validation

Date: 2026-05-11
Host: pve07
VM: 999 (`routerd-live-test`, destroyed after validation)
ISO: `dist/iso/routerd-live-v20260511.1240.iso`

## VM setup

Created a temporary PVE VM with serial console:

- CPU: 2 cores
- memory: 1536 MiB
- ISO: `local:iso/routerd-live-v20260511.1240.iso`
- WAN: `net0` on `vmbr0`
- LAN: `net1` on `vmbr3`
- persistence disk: 1 GiB `scsi0`, partitioned as FAT32/vfat label `ROUTERD`
- console: `serial0 socket`, accessed with `qm terminal 999`

`vmbr490` was not present on pve07, so `vmbr3` was used as an isolated LAN-side bridge for the boot and persistence test.

## Boot and wizard

Serial boot succeeded. Alpine booted and presented the login prompt on `ttyS0`.

The live profile printed the routerd MOTD, installed dependencies through `apk`, and started the configure wizard.

Wizard answers:

- router name: `routerd-live-test`
- WAN: `eth0`, DHCP
- default DNS fallback: `1.1.1.1`
- LAN: `eth1`, `192.168.99.1/24`
- DHCPv4 server: enabled, pool `192.168.99.100-192.168.99.200`
- DHCPv6 / RA: disabled for this minimal test
- DNS resolver: enabled
- NTP server: enabled
- firewall: enabled
- NAT44: enabled
- management: LAN
- USB persistence: enabled on `/dev/sda1`
- USB flush: enabled
- tmpfs log limit: `100M`

`routerd validate` and `routerd apply --once` completed with `Healthy`, generation `1`, resource count `14`.

## USB persistence

The persistence disk was formatted in the live VM:

```text
/dev/sda1 256M part vfat ROUTERD
```

The wizard saved config to USB:

```text
routerd-live: mounted /dev/sda1 on /media/routerd-usb with rw,async,noatime,utf8,shortname=mixed
routerd-live: saved routerd config to /dev/sda1
```

Files after save:

```text
/media/routerd-usb/routerd/log-limit
/media/routerd-usb/routerd/router.yaml
/media/routerd-usb/routerd/usb-device
/media/routerd-usb/routerd/usb-flush-enabled
```

Manual flush succeeded:

```text
routerd-live: flushed routerd config, state, and log archive to /dev/sda1
```

Files after flush included:

```text
/media/routerd-usb/routerd/logs/20260511-044857.tgz
/media/routerd-usb/routerd/state/routerd-varlib.tgz
```

## Removal simulation

Simulated unexpected USB removal inside the VM:

```sh
echo 1 > /sys/block/sda/device/delete
```

The helper reported the expected warning and routerd stayed healthy:

```text
routerd-live: warning: USB persistence device /dev/sda1 is no longer present; keeping runtime state in RAM
routerctl status: Healthy generation 1 resourceCount 14
```

## Reboot restore

After rebooting the VM, `/dev/disk/by-label/ROUTERD` was discovered and the saved config was restored automatically:

```text
routerd-live: restored /usr/local/etc/routerd/router.yaml from /dev/disk/by-label/ROUTERD
CONFIG_RESTORED
routerctl status: Healthy generation 1 resourceCount 14
routerd-live: USB persistence mounted: /dev/disk/by-label/ROUTERD -> /media/routerd-usb
```

The configure wizard did not run again after the config was restored.

## Cleanup

The temporary VM was stopped and destroyed:

```text
VM999_CLEANED
```

## Caveat

This A4 validation used serial-console output as evidence. The VM used `--vga serial0`, so QEMU `screendump` had no graphical console to capture. For tutorial screenshots, create a separate VM with a graphical VGA device or capture serial output as text blocks.
