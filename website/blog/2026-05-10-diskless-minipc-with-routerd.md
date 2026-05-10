---
slug: diskless-minipc-with-routerd
title: From virtual SDN routing to a diskless mini PC router
tags: [routerd, live-iso, diskless, proxmox, router]
---

routerd is built for an unusually wide deployment spectrum.

At one end, it can route between virtual SDN or VNET segments in a lab. At the
other end, it can boot a small physical mini PC from a live ISO, save its
configuration on USB, buffer logs in RAM, and become a persistent home router
without installing an operating system to an internal disk.

That range is the point. Real routers are not just packet forwarding. They are
DHCP, DNS, prefix delegation, tunnels, NAT, firewall policy, health checks,
logs, service units, sysctl values, and recovery behavior. routerd keeps those
parts in one typed resource graph.

![Diskless mini PC flow](/img/routerd-diskless-minipc.svg)

<!-- truncate -->

## Why a diskless router?

Small N100 or NUC-like PCs are good router hardware. They are fast enough for a
serious home network, inexpensive, and easy to replace. But many users do not
want another full Linux installation to maintain.

The routerd live ISO gives you a middle ground:

- boot from ISO
- answer a text wizard
- store `router.yaml` on a USB stick
- keep logs in tmpfs
- flush compressed logs and state to USB once per day
- reboot back into the same router state

It is close to an appliance experience, but the source of truth is still a
normal YAML file you can read, copy, and version.

## Try it in Proxmox VE first

The same ISO works well as a lab VM. Create a VM with one WAN NIC and one
isolated LAN NIC, mount `routerd-live.iso`, and use the serial console:

![PVE VM creation placeholder](/img/tutorials/diskless-01-pve-vm-create.svg)

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

The wizard runs on both framebuffer and serial consoles:

![Serial console placeholder](/img/tutorials/diskless-03-serial-console.svg)

## Persist only what matters

When you enable USB persistence, routerd writes this small layout to the USB
partition:

```text
routerd/
  router.yaml
  logs/
  state/
```

The live helper detects `ext4`, `vfat`, and `exfat`, mounts with
`async,noatime` by default, and uses Alpine `lbu` to preserve selected local
state.

![USB persistence placeholder](/img/tutorials/diskless-05-usb-persistence.svg)

## Check the result

After the wizard applies the generated config, `routerctl status` should report
a healthy router:

![routerctl status placeholder](/img/tutorials/diskless-06-routerctl-status.svg)

Then attach a LAN client and check DNS plus outbound HTTPS:

![Client curl placeholder](/img/tutorials/diskless-07-client-curl.svg)

For the full step-by-step guide, see
[Diskless mini PC walkthrough](/docs/tutorials/diskless-minipc-walkthrough).
