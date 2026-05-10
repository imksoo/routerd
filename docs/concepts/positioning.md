---
title: Positioning
slug: /concepts/positioning
---

# Positioning

routerd is for people who want a router that is understandable from its
configuration and observable from its runtime state.

It is not a replacement for a full network operating system. It is also not a
cloud controller that owns many routers from the outside. routerd runs locally
on one router host and turns typed YAML resources into host networking,
services, routing, tunnels, firewall rules, logs, and status.

## What routerd optimizes for

routerd optimizes for small and medium networks where the operator wants:

- a declarative router configuration that can live in git
- local operation without a hosted controller
- explicit ownership of generated host artifacts
- event-driven status instead of hidden daemon state
- safe management-path checks before applying risky changes
- observability that explains why a route, tunnel, or firewall decision exists

Typical users are home lab operators, small office operators, developers who
run Proxmox VE or KVM, and people replacing hand-maintained Linux router
scripts with something more repeatable.

## Spectrum coverage

routerd intentionally covers a wide spectrum of router work:

| Area | Examples |
| --- | --- |
| WAN access | DHCPv4, DHCPv6-PD, DHCPv6 information request, PPPoE |
| IPv4 transition | DS-Lite, NAT44, multi-stage WAN fallback |
| LAN service | DHCPv4, DHCPv6, RA, DNS, NTP |
| Routing | static routes, policy routes, EgressRoutePolicy, health checks |
| Security | three-role firewall model, guest mode, denial logging |
| Overlay | WireGuard, Tailscale integration, VXLAN groundwork, VRF |
| Operations | Web Console, `routerctl`, OpenTelemetry, log stores |
| Bootstrap | packages, sysctl profiles, systemd units, live ISO |

The breadth matters because routers fail at the boundaries. A DNS choice may
depend on a DHCPv6 information option. A DS-Lite tunnel may depend on an AFTR
record that only resolves through a specific upstream. A route should not
become primary until a health check has observed it. routerd keeps those
relationships in one resource graph.

## Compared with shell scripts

Shell scripts are easy to start and hard to audit later. They often answer
"what command did we run?" but not "what state should exist now?"

routerd keeps the desired state in YAML, stores observed state, emits events,
and exposes the result through an API, CLI, and Web Console. That makes it
easier to inspect drift, compare generations, and debug real traffic.

## Compared with appliance firmware

Appliance firmware is convenient when the use case fits its UI. It becomes
harder when you need a precise mix of DS-Lite, PPPoE fallback, local DNS,
custom firewall behavior, OpenTelemetry, or a lab overlay network.

routerd keeps those features as resources. The UI is for reading and
debugging. Configuration changes remain CLI and YAML driven.

## Compared with Kubernetes-style controllers

routerd borrows resource and controller ideas, but it does not require a
cluster. The host is the boundary. The kernel, local daemons, and local files
are the things being adjusted.

That keeps the operational model small enough for a home router while still
allowing event-driven coordination between DHCP, DNS, tunnels, health checks,
routes, firewall logs, and telemetry.

## Non-goals

routerd does not currently aim to be:

- a hosted SDN controller
- a remote plugin marketplace
- a general-purpose firewall language
- a replacement for every enterprise router feature
- a GUI-first configuration system

The project favors explicit YAML, local control, and high-quality operational
feedback over a broad clickable management surface.
