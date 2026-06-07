---
title: Tutorials
slug: /tutorials
---

# Tutorials

![Diagram showing the routerd tutorial path from install, getting started, and diskless live ISO to WAN services, LAN services, firewall, NixOS, and FreeBSD](/img/diagrams/tutorial-index.png)

## Diskless mini PC router in five minutes

Boot the routerd live ISO, answer the text wizard, save the configuration to a
USB stick, and turn a small x86 mini PC into a persistent router without
installing an OS to the internal disk.

[Start the diskless walkthrough](/docs/tutorials/diskless-minipc-walkthrough)

![Diskless mini PC flow](/img/routerd-diskless-minipc.svg)

## Pick a path

| Goal | Tutorial |
| --- | --- |
| Install routerd from a release archive | [Install](/docs/tutorials/install) |
| Build a first router from YAML | [Getting started](/docs/tutorials/getting-started) |
| Configure WAN-side acquisition and tunnels | [WAN-side services](/docs/tutorials/wan-side-services) |
| Configure DHCP, DNS, RA, and NTP on LAN | [LAN-side services](/docs/tutorials/lan-side-services) |
| Add a conservative firewall baseline | [Basic firewall](/docs/tutorials/basic-firewall) |
| Start from NixOS | [NixOS getting started](/docs/tutorials/nixos-getting-started) |
| Start from FreeBSD | [FreeBSD getting started](/docs/tutorials/freebsd-getting-started) |

routerd is unusual because the same resource model can describe a virtual lab
router between SDN/VNET segments and a diskless physical router on a mini PC.
Use the tutorial that matches your first deployment, then reuse the same
resources as the network grows.
