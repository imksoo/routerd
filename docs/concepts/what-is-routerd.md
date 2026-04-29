---
title: What is routerd?
slug: /concepts/what-is-routerd
sidebar_position: 1
---

# What is routerd?

routerd is a small declarative control plane for **router hosts**. You write
the desired router behavior in YAML, run `routerd apply`, and the daemon
brings the live host into that shape: interfaces, addresses, DHCP and DNS
service, NAT, policy routing, firewall, and route health checks all change
together from one config.

This page explains what routerd is, what problem it solves, and where it
fits relative to other tools. If you want to start using it instead, jump
to the [Tutorials](../tutorials/install).

## The problem routerd addresses

A router host configured "by hand" has a sprawling surface area:

- `/etc/netplan/*.yaml` for interfaces and addressing
- `dnsmasq.conf` for LAN DHCP, DHCPv6, RA, and DNS
- `nftables.conf` for NAT and firewall
- `dhclient` / `dhcp6c` / `systemd-networkd` for WAN DHCP/PD
- `sysctl.conf` for IP forwarding, accept_ra
- `systemd-timesyncd`, hostname, and so on

Each of these is configured separately, often by ad-hoc shell commands.
Reproducing the same router on a second host means walking the operator
through the same sequence of edits and hoping nothing was missed. There is
no single artifact that says "this is the router."

## What routerd does

routerd treats the router as a **single resource graph**. A router YAML
declares typed resources (interfaces, DHCP clients, NAT rules, firewall
zones, etc.) and a single `routerd apply` makes the host match.

Concretely, routerd:

- Reads a YAML file with a `Router` resource and a list of resources.
- Validates the file (`routerd validate`).
- Plans the changes against the live host (`routerd apply --dry-run`).
- Applies them (`routerd apply`).
- Records what it observed and what it owns in a local SQLite database.
- Repeats the apply periodically as a daemon to keep the host converged.

The YAML file is the only artifact you need to recreate the router. It
lives in git; reviews are on diffs; rollbacks are on commits.

## What routerd is not

- **Not a generic configuration management system.** routerd has typed
  resources for router behavior only. It does not try to be Ansible or
  Puppet.
- **Not a Linux distribution or appliance.** routerd runs on top of an
  existing Linux or FreeBSD host and uses the host's own daemons
  (systemd-networkd, dnsmasq, nftables, KAME `dhcp6c`, etc.).
- **Not a remote API.** There is no central control plane. routerd runs
  locally on each router host. There is a small local control socket for
  `routerctl` to talk to the daemon.

## Who routerd is for

- Operators who run a small number of "real" routers (home, lab, branch
  office) and want them to be reviewable and reproducible.
- People who already write infrastructure as code for the rest of their
  stack and want the router included.
- People who want to switch hosts (Ubuntu → NixOS, swap a NIC) without
  rewriting the network configuration.

## Where to go next

- [Design philosophy](./design-philosophy) — the principles behind routerd.
- [Resource model](./resource-model) — `Router`, `Resource`, `kind`,
  `metadata.name`.
- [Apply and render](./apply-and-render) — the verbs you use day to day.
- [State and ownership](./state-and-ownership) — what routerd remembers.
- [Install](../tutorials/install) — start using routerd.
