---
title: State and ownership
slug: /concepts/state-and-ownership
sidebar_position: 5
---

# State and ownership

routerd separates declared intent from observed state.
YAML is the intent you manage.
SQLite, lease files, and `events.jsonl` are state that routerd and its dedicated daemons observe.

## Where state lives

Linux defaults:

| Kind | Example |
| --- | --- |
| routerd state database | `/var/lib/routerd/routerd.db` |
| DHCPv6-PD lease | `/var/lib/routerd/dhcpv6-client/wan-pd/lease.json` |
| DHCPv4 lease | `/var/lib/routerd/dhcpv4-client/wan/lease.json` |
| PPPoE state | `/var/lib/routerd/pppoe-client/<name>/state.json` |
| HealthCheck state | `/var/lib/routerd/healthcheck/<name>/state.json` |
| Runtime sockets | `/run/routerd/.../*.sock` |

FreeBSD layouts use `/var/run` and `/var/db` instead.

## Ownership

Every host-side artifact routerd creates has an owning resource.
For example, the dnsmasq configuration is owned by the DHCP and RA resources, the `routerd-dns-resolver` configuration by `DNSResolver` and `DNSZone`, and the nftables NAT table by `NAT44Rule`.

Knowing the owner answers three questions:

- Is this artifact safe for routerd to modify?
- When the resource is removed from YAML, should routerd remove the host-side artifact too?
- Does routerd adopt an existing object, or create a new one from scratch?

## Don't act on stale state

Leases and observed values are useful, but acting on stale data is dangerous.
DHCPv6-PD prefixes in particular are propagated downstream only while they are confirmed `Bound`.
When that confirmation is missing, routerd suppresses the matching AAAA records, RA, DHCPv6 server, and LAN IPv6 address applications instead of advertising broken connectivity.

## Events

routerd and its daemons record state changes as events.
Events are persisted in the SQLite `events` table and in per-daemon `events.jsonl` files.
`EventRule` and `DerivedEvent` consume that stream to synthesize virtual state changes.
