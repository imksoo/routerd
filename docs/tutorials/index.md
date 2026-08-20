---
title: Tutorials
slug: /tutorials
---

# Tutorials

![Diagram showing the routerd tutorial path from safe lab and network basics through first router, WAN services, LAN services, and advanced deployment options](/img/diagrams/tutorial-index.png)

Start with one small, observable goal. Do not start with a production router or
an ISP-specific configuration.

## First path: an isolated Ubuntu lab

1. [Network basics](./network-basics.md) — learn WAN, LAN, DHCP, DNS, NAT, and
   the shape of a routerd resource.
2. [Install and upgrade](../install-and-upgrade.md) — install the release on an
   Ubuntu Server VM or spare computer.
3. [Getting started safely](./getting-started.md) — validate and dry-run a file
   before any live change.
4. [Bring up the first lab router](./first-router.md) — test DHCP and IPv4 NAT
   with a client on an isolated LAN.
5. [LAN-side services](./lan-side-services.md) and
   [WAN-side services](./wan-side-services.md) — add one feature at a time.

## Choose the next tutorial by goal

| Goal | Read this next | Prerequisite |
| --- | --- | --- |
| Add local DHCP, DNS, RA, or NTP | [LAN-side services](./lan-side-services.md) | Working isolated LAN |
| Add DHCPv6-PD, PPPoE, or DS-Lite | [WAN-side services](./wan-side-services.md) | ISP-specific facts and a recovery path |
| Understand current firewall resources | [Basic NAT and firewall policy](./basic-firewall.md) | Do not use as the only Internet security boundary |
| Use a diskless mini PC | [Diskless mini PC walkthrough](./diskless-minipc-walkthrough.md) | A removable USB you can identify safely |
| Start from FreeBSD | [FreeBSD getting started](./freebsd-getting-started.md) | Feature support review; Ubuntu is the first-lab target |

## Advanced examples come later

The configuration examples for DS-Lite, multi-WAN, WireGuard, Tailscale,
CloudEdge SAM, Kubernetes, and high availability solve real problems, but each
assumes a working basic router first. Read their prerequisites before applying
them. A sample is a map of one situation, not a safe default for every network.
