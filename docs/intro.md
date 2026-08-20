---
title: Documentation
slug: /
sidebar_position: 0
sidebar_label: Overview
---

# routerd documentation

![Diagram showing the routerd documentation map from a safe first lab through concepts, examples, operations, and reference material](/img/diagrams/intro.png)

routerd describes a router in YAML, checks the description, and applies the
matching host configuration. A router is a computer that connects networks. In
routerd, one file can describe the WAN side, the LAN side, DHCP, DNS, routes,
and selected services instead of scattering the intent across many unrelated
files.

:::caution Start in a lab
For a first live test, use an isolated Ubuntu Server VM or a spare computer and
keep a console or separate management network. A live apply can change routes,
addresses, and services. Do not begin with the only router for a home, school,
or workplace.
:::

:::tip Recommended stable milestone
For a new deployment, start from the recommended stable milestone
**v20260707.1514**. The current release may contain newer work; the
[Stable milestone](./releases/stable.md) page explains why this milestone is
the production recommendation.
:::

## First route for a new reader

1. [Network basics](./tutorials/network-basics.md) — WAN, LAN, DHCP, DNS, NAT,
   and the `/24` notation in plain language.
2. [Install and upgrade](./install-and-upgrade.md) — install routerd on an
   isolated Ubuntu Server lab host.
3. [Getting started safely](./tutorials/getting-started.md) — validate and
   preview a file before any live network change.
4. [Bring up the first lab router](./tutorials/first-router.md) — test DHCP and
   IPv4 NAT with one isolated LAN client.
5. [Resource model](./concepts/resource-model.md) — learn how a routerd file
   links its named resources together.

Ubuntu Server is the primary, most exercised target. FreeBSD and NixOS include
groundwork and selected integration paths, but are not the recommended first
platform. See [Supported platforms](./platforms.md) before choosing one.

## Find the page for your goal

| If you want to… | Start here |
| --- | --- |
| Install or upgrade routerd | [Install and upgrade](./install-and-upgrade.md) |
| Learn basic networking words first | [Network basics](./tutorials/network-basics.md) |
| Make a first safe lab configuration | [Getting started safely](./tutorials/getting-started.md) |
| Build a working DHCP/NAT lab router | [Bring up the first lab router](./tutorials/first-router.md) |
| Understand what routerd is and why it exists | [What is routerd?](./concepts/what-is-routerd.md) |
| Generate a starter config in the browser | [routerd config wizard](https://routerd.net/wizard) |
| Enable editor completion and validation | [VS Code YAML schema](./how-to/vscode-yaml-schema.md) |
| Operate a running router | [Reconcile](./operations/reconcile.md) |
| Look up a resource kind or field | [Resource API](./api-v1alpha1.md) |
| Read release changes | [Changelog](./releases/changelog.md) |

## How the documentation is organized

- **Start** — a safe first path and the vocabulary needed to follow it.
- **Learn** — concepts such as resource ownership, rendering, and reconcile.
- **Build** — focused features such as DNS, firewall groundwork, and multi-WAN.
- **Configuration examples** — complete examples with prerequisites and limits.
- **Tutorials and how-to guides** — a practical task with checks and safe
  stopping points.
- **Operate and reference** — status, state, platform support, and API details.

If a page uses a word you do not know, follow its first link or use the
[glossary](./concepts/glossary.md). It is safer to understand one small step
than to combine several advanced examples at once.
