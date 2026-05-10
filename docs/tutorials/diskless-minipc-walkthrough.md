---
title: Diskless mini PC walkthrough
---

# Diskless mini PC walkthrough

This tutorial turns a small x86 mini PC into a router without installing an OS
to its internal disk. The router boots the routerd live ISO, stores
configuration on USB, buffers logs in RAM, and flushes a compact archive to USB
once per day.

![Diskless mini PC flow](/img/routerd-diskless-minipc.svg)

## What you need

- A mini PC with at least two network interfaces.
- A USB stick for routerd persistence.
- The latest `routerd-live.iso`.
- Console access. On Proxmox VE, `qm terminal` works through the serial console.
- A WAN network that can provide DHCPv4, or a static WAN address.
- A LAN switch or isolated test bridge.

## 1. Prepare the USB stick

Create one partition and format it with a filesystem the live ISO can mount.
Label it `ROUTERD` so the ISO can find it automatically.

Example from a Linux workstation:

```sh
sudo mkfs.ext4 -L ROUTERD /dev/sdX1
```

Replace `/dev/sdX1` with the actual USB partition. Do not format the wrong
device.

## 2. Boot the live ISO

Download the fixed latest URL:

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-live.iso
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-live.iso.sha256
sha256sum -c routerd-live.iso.sha256
```

Boot the mini PC from the ISO. The same image works on a video console and a
serial console.

For Proxmox VE:

```sh
qm create 200 \
  --name routerd-live-demo \
  --memory 1536 \
  --cores 2 \
  --ostype l26 \
  --serial0 socket \
  --vga serial0 \
  --boot order=ide2 \
  --ide2 local:iso/routerd-live.iso,media=cdrom \
  --net0 virtio,bridge=vmbr0 \
  --net1 virtio,bridge=vmbr490
qm start 200
qm terminal 200
```

Use an isolated LAN bridge for early DHCP and RA testing.

## 3. Run the wizard

Log in as `root`. The live ISO starts the setup wizard.

The wizard asks for:

- router name
- WAN interface
- WAN IPv4 mode
- LAN interface
- LAN address
- DHCPv4, DNS, NTP, RA, firewall, and NAT44 choices
- management placement
- USB persistence

When asked about USB persistence, choose `yes` and select the USB partition.
If the partition is labeled `ROUTERD`, it should be listed automatically.

Enable the daily USB flush job unless you are only testing. The default log
buffer is 100 MiB under `/run/routerd/logs`.

## 4. Confirm the first apply

After confirmation, the wizard writes:

```text
/usr/local/etc/routerd/router.yaml
```

It then runs:

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once
```

Check status:

```sh
routerctl status
```

The phase should become `Healthy`.

## 5. Test a LAN client

Connect a client to the LAN interface or test bridge.

The client should receive:

- an IPv4 address from the configured DHCPv4 pool
- default route through the router
- DNS server pointing at the router
- NTP server pointing at the router, when enabled

Basic checks:

```sh
dig @192.168.10.1 www.google.com A +short
curl -4 https://www.google.com/generate_204
```

Adjust the address if you chose a different LAN prefix.

## 6. Reboot and confirm persistence

Reboot the mini PC with the USB stick still attached.

At boot, the live ISO:

1. mounts the USB device
2. restores `routerd/router.yaml`
3. prepares `/run/routerd/logs` as tmpfs
4. applies the router configuration
5. starts the live routerd daemon

Log in and run:

```sh
routerctl status
```

The router should converge without rerunning the wizard.

## 7. How log persistence works

Logs are written to RAM first:

```text
/run/routerd/logs
```

The daily flush job copies these artifacts to USB:

- current `router.yaml`
- routerd state snapshot
- compressed log archive

This avoids constant writes to USB flash. If the tmpfs buffer exceeds the
configured limit, older files are removed first.

You can flush manually:

```sh
/usr/share/routerd/live-persistence.sh flush
```

## Troubleshooting

### The wizard does not list the USB stick

Check the partition from the shell:

```sh
blkid
lsblk -f
```

If needed, pass the device explicitly on the kernel command line:

```text
routerd.usb=/dev/sdb1
```

### The router boots into wizard mode again

The ISO did not find a saved config. Mount the USB device and check:

```sh
mount /dev/sdX1 /mnt
ls -l /mnt/routerd/router.yaml
```

### Logs are missing after reboot

Logs are buffered in RAM. They persist only after the daily flush job runs or
after a manual flush.

### The LAN client has no address

Check that the LAN interface is the one selected in the wizard:

```sh
routerctl status --json
ip addr
```

If you are testing in Proxmox VE, confirm the client and router LAN NIC are on
the same isolated bridge.
