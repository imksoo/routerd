---
title: Design philosophy
slug: /concepts/design-philosophy
sidebar_position: 2
---

# Design philosophy

routerd treats a router as a set of stateful resources, not a pile of configuration files. This page explains the principles behind the implementation choices.

## YAML is the centre of intent

The YAML configuration expresses the router's intent. routerd compares that intent to the host's current state and applies only the necessary diff. The configuration is something you can re-read, not a memory of the steps you ran.

## Stateful work belongs in dedicated daemons

DHCPv6-PD, DHCPv4, PPPoE, and health checks have timers, recover from restarts, and produce event histories. Squeezing them into one-shot commands makes lease renewals and incident debugging unstable.

routerd runs that stateful work as small dedicated daemons. Each daemon persists its lease or internal state to a file and exposes its status over a Unix domain socket. The main routerd process consumes that status to reconcile downstream resources.

## Do not announce broken IPv6 to the LAN

If DHCPv6-PD has been lost but the router keeps emitting RA, AAAA records, and LAN addresses derived from the old prefix, clients see "IPv6 looks present but does not work." routerd is designed to **stop** announcing IPv6 to downstream when the prefix's status cannot be confirmed.

## Compose small parts with events

routerd does not handle the entire router in one big procedure. It connects small controllers with events. When DHCPv6-PD becomes `Bound`, the LAN address, RA, DHCPv6 server, DNS answers, DS-Lite, and IPv4 routes converge in turn.

This shape makes it much easier to find which step stopped progressing.

## Confine OS differences

The same intent maps to different host-level expressions on Linux, NixOS, and FreeBSD. routerd confines those differences inside `pkg/platform` feature flags and per-OS renderers. The user-visible resource names stay as similar as possible across platforms.

## Prefer correct names over compatibility (for now)

routerd is at the v1alpha1 stage. We previously renamed the DHCP-related kinds and binaries to align with RFC notation (`DHCPv4*` / `DHCPv6*`) without keeping aliases. At the pre-release stage, we prioritise avoiding wrong names that would otherwise live forever.
