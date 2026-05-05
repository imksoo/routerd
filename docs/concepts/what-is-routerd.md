---
title: What is routerd?
slug: /concepts/what-is-routerd
sidebar_position: 1
---

# What is routerd?

routerd is a declarative router control plane for Linux hosts, with NixOS and
FreeBSD groundwork in progress. You write the router intent as YAML resources.
routerd turns that intent into interfaces, addresses, DHCP service, DNS service,
NAT, routes, tunnels, health checks, system packages, sysctl values, service
units, logs, and status.

routerd is not a distribution and it is not a hosted controller. It runs on each
router host. It uses local kernel features and host components such as
systemd-networkd, dnsmasq, nftables, pppd, WireGuard, and systemd where that is
the right boundary.

## The Problem

A hand-built router spreads state across many places:

- interface addresses in netplan, systemd-networkd, rc.d, or NixOS settings
- DHCP, DHCPv6, DHCP relay, and RA in dnsmasq configuration
- DNS forwarding and local records in resolver-specific files
- NAT, route policy, conntrack, and firewall state in nftables and iproute2
- DHCPv4, DHCPv6-PD, PPPoE, health checks, and logging in separate daemons
- packages, sysctl values, and service units in host bootstrap scripts

routerd treats these pieces as resources. The YAML shows the router intent.
Git diffs show the operational change. `routerctl` and the Web Console show
what the host actually observed.

## Current Shape

`routerd serve` loads resources, resolves dependencies, starts child daemons,
subscribes to events, and adjusts the host toward the desired state.

Long-running protocol state lives in small managed daemons:

- `routerd-dhcpv6-client` handles DHCPv6 prefix delegation and information
  request.
- `routerd-dhcpv4-client` handles DHCPv4 WAN leases.
- `routerd-pppoe-client` handles PPPoE sessions.
- `routerd-healthcheck` runs TCP, DNS, HTTP, and ICMP probes.
- `routerd-dns-resolver` answers DNS zones and forwards DoH, DoT, DoQ, and UDP
  upstreams.
- `routerd-dhcp-event-relay` converts dnsmasq lease changes into routerd events.
- `routerd-firewall-logger` imports firewall logs into routerd log storage.

Each daemon exposes local HTTP+JSON status over a Unix socket and persists its
own state where needed. routerd consumes those events and updates LAN service,
DNS records, DS-Lite tunnels, NAT, route policy, health-derived choices, and
observability stores.

## What It Can Manage

The current implementation can manage:

- DHCPv6-PD and delegated IPv6 LAN addresses
- DHCPv6 information request, AFTR DNS resolution, and DS-Lite
- DHCPv4 WAN leases and DHCPv4 LAN scopes with reservations
- DHCPv6 server modes and IPv6 Router Advertisement options
- DNS zones, DHCP-derived records, conditional forwarding, DoH, DoT, DoQ,
  UDP fallback, multiple listen profiles, and cache
- NAT44, private-destination exclusions, IPv4 route policy, reverse-path
  filter settings, Path MTU policy, and TCP MSS clamping
- PPPoE, WireGuard, VXLAN, VRF, and cloud-oriented IPsec configuration
  groundwork
- package installation, sysctl profiles, network adoption, systemd units,
  NTP client configuration, log sinks, log retention, and Web Console
- `EgressRoutePolicy`, `HealthCheck`, `EventRule`, and `DerivedEvent`
  coordination
- status, event, DNS query, connection, traffic-flow, and firewall-log
  inspection

## What Is Still Deliberately Narrow

routerd is v1alpha1 pre-release software. Names and fields may change without a
compatibility alias when the cleanup makes the router safer or the configuration
more understandable.

Stateful firewall filtering exists as groundwork, but routerd is not yet a
general-purpose firewall rule language. NixOS and FreeBSD support is active,
but not at full Ubuntu parity. Platform-specific claims are tracked in the
platform matrix.

## Next Pages

- [Design philosophy](./design-philosophy)
- [Resource model](./resource-model)
- [Apply and render](./apply-and-render)
- [State and ownership](./state-and-ownership)
- [Install](../tutorials/install)
