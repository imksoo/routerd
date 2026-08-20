---
title: Install (first-lab shortcut)
sidebar_position: 1
---

# Install routerd for a first lab

The authoritative installation and upgrade instructions are in
[Install and upgrade](../install-and-upgrade.md). This short page exists so a
reader following the tutorial list reaches that one source of truth.

For a first experiment, choose an isolated Ubuntu Server VM or a spare computer
with console access. Ubuntu Server is the primary target. FreeBSD and NixOS are
second-tier groundwork and should not be the first platform you use to learn
the workflow.

After installation, continue in this order:

1. [Network basics](./network-basics.md)
2. [Getting started safely](./getting-started.md)
3. [Bring up the first lab router](./first-router.md)

Do not run a live apply over the only SSH connection to a router. The install
page explains how to validate and dry-run a file before starting the service.
