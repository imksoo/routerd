---
title: DNS resolver
slug: /concepts/dns-resolver
---

# DNS resolver

Phase 2.0 splits routerd DNS into two resource kinds.

`DNSZone` owns local authoritative data.
It stores manual records and records derived from DHCP leases.

`DNSResolver` owns the daemon instance.
It defines listen addresses, source ordering, upstream selection, and cache policy.
One `DNSResolver` resource starts one `routerd-dns-resolver` process.

## Source ordering

`DNSResolver.spec.sources` is evaluated in order.
A `zone` source answers from `DNSZone`.
A `forward` source sends matching zones to a selected upstream.
An `upstream` source handles the default recursive path.

The resolver supports DoH, DoT, DoQ, and plain UDP DNS.
It tries upstreams by priority and falls back when a higher source fails.

## Multiple listen profiles

`spec.listen` is a list.
Each entry can select a subset of source names.
This allows LAN and VPN listeners to behave differently while sharing one resolver resource.

## Network-constrained upstreams

`sources[].viaInterface` binds outgoing queries to a specific interface on Linux.
The value can reference `Interface`, `WireGuardInterface`, `IPsecConnection`, or `VRF` status.

`sources[].bootstrapResolver` supplies DNS server addresses for resolving DoH and DoT endpoint names.
This is useful when the endpoint name is only resolvable inside an access network.

## dnsmasq boundary

dnsmasq is now limited to DHCPv4, DHCPv6, DHCP relay, and RA.
It does not generate `server=`, `local=`, or `host-record=` lines.
All DNS answering and forwarding goes through `routerd-dns-resolver`.
