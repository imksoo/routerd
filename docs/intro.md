---
title: Documentation
slug: /
sidebar_position: 0
sidebar_label: Overview
---

# routerd documentation

routerd turns typed YAML resources into a working, observable router on a Linux, NixOS, or FreeBSD host. Pick the section that matches what you are trying to do.

:::tip Recommended stable release
For a new deployment, start from the recommended stable milestone **v20260528.2308**. See [Stable milestone](./releases/stable.md) for details.
:::

## By goal

| If you want to… | Start here |
| --- | --- |
| Install or upgrade routerd | [Install and upgrade](./install-and-upgrade.md) |
| Understand what routerd is and why it exists | [Concepts → What is routerd](./concepts/what-is-routerd.md) |
| Understand where routerd fits | [Concepts → Positioning](./concepts/positioning.md) |
| Stand up a router for the first time | [Tutorials → Getting started](./tutorials/getting-started.md) |
| Try a diskless mini PC router | [Tutorials → Diskless mini PC walkthrough](./tutorials/diskless-minipc-walkthrough.md) |
| Solve a specific deployment problem | [How-to guides](./how-to/multi-wan.md) |
| Look up a resource kind or field | [Reference → Resource API](./api-v1alpha1.md) |
| Operate a running router | [Operations → Reconcile](/docs/operations/reconcile) |
| Read background notes on hard cases | [Knowledge base](./knowledge-base/dhcpv6-pd-clients.md) |
| Catch up on what changed | [Releases → Changelog](./releases/changelog.md) |

## All sections

- **Concepts** — positioning, vision, design philosophy, resource model, ownership semantics
- **Install and upgrade** — release archive install, package dependencies, upgrade, uninstall
- **Tutorials** — diskless mini PC, first router, WAN/LAN services, basic firewall, NixOS quickstart
- **How-to** — multi-WAN, FLET'S setup, PVE overlay, OpenTelemetry export, troubleshooting
- **Knowledge base** — field notes from real deployments (DHCPv6-PD clients, NTT NGN PD acquisition)
- **Reference** — Resource API, control API, plugin protocol, supported platforms, hardware, ownership rules
- **Operations** — reconcile and removal, state database, host inventory
- **Design notes** — open architectural questions and design rationales
- **Releases** — changelog
