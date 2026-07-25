---
title: Why routerd ships its own DHCPv6-PD client
---

# Why routerd ships its own DHCPv6-PD client

![Diagram showing why routerd owns DHCPv6-PD from OS client variation and stale prefix risk through routerd-dhcpv6-client lease state, status, delegated LAN address inputs, and HA DUID operation](/img/diagrams/knowledge-base-dhcpv6-pd-clients.png)

routerd's current approach is to handle DHCPv6-PD with the dedicated `routerd-dhcpv6-client` daemon. The OS-bundled clients we evaluated earlier are not part of the supported configuration today.

## Rationale

DHCPv6 prefix delegation is more than acquiring a prefix. It involves renewal, restart recovery, and event recording. Generating configuration for an OS-bundled client did not let us cleanly tie those things back to routerd's state model and downstream LAN services.

Owning the daemon lets routerd:

- Persist the lease in `lease.json`.
- Restore it at startup.
- Record renewal results as events.
- Expose `Bound` / `Pending` over `/v1/status`.
- Emit events that other controllers (LAN address derivation, RA, DHCPv6 server, DS-Lite, DNS) consume to converge.

## Binary and paths

```text
routerd-dhcpv6-client
```

| Path | Purpose |
| --- | --- |
| `/run/routerd/dhcpv6-client/<name>.sock` | per-resource control socket |
| `/var/lib/routerd/dhcpv6-client/<name>/lease.json` | persisted lease |
| `/var/lib/routerd/dhcpv6-client/<name>/events.jsonl` | append-only event log |

## What was evaluated and dropped

We compared `systemd-networkd`, WIDE/KAME-style clients, and several other DHCP clients before settling on a routerd-owned daemon. Those investigations remain interesting context but are not part of the current shipped configuration.

The current resource is `DHCPv6PrefixDelegation`. There is intentionally no `client` field for selecting an OS-bundled implementation.

## Operational reminders

Do not run more than one DHCPv6-PD client on the same WAN interface. Two simultaneous clients can confuse the upstream and stop Reply messages.

When migrating to routerd, first stop the old client (along with its lease files and any old systemd / rc.d configuration that brought it up). Then start `routerd-dhcpv6-client`.

## HA and WAN link-layer identity

`spec.clientDUID` can keep the DHCPv6 client identity stable across an active/standby pair, but it does not move the WAN interface's Ethernet MAC address. Some residential gateways associate the delegated prefix's return path with the Ethernet MAC observed from the DHCPv6 client. In that environment, a standby promotion can report a `Bound` lease and still fail to receive return traffic for addresses from the delegated prefix.

Do not work around this by assigning one shared MAC address to both physical WAN interfaces: both nodes can be present on the same L2 segment, so that creates a duplicate-MAC failure mode. A correct HA design needs one WAN virtual MAC that moves with the active node, and must bind the DHCPv6 client to the interface carrying that virtual MAC.

On Linux, set `spec.vrrp.useVirtualMAC: true` and a dot-free `spec.vrrp.virtualMACInterface` on the WAN `VirtualAddress`. keepalived then moves a named VRRP VMAC with the MASTER. Bind DHCPv6-PD, DHCPv6 information, and DS-Lite tunnels to that VMAC interface, while retaining the physical WAN as the VRRP parent. routerd creates the VMAC's EUI-64 link-local IPv6 address before starting its DHCPv6 client.

Until that support is available, verify both the DHCPv6-PD control plane and the data plane after every promotion:

```sh
routerctl describe DHCPv6PrefixDelegation/wan-pd
ip -6 route get 2606:4700:4700::1111 from <delegated-lan-address>
ping -6 -I <delegated-lan-address> -c 3 2606:4700:4700::1111
```
