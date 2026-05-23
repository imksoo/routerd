---
title: Why routerd ships its own DHCPv6-PD client
---

# Why routerd ships its own DHCPv6-PD client

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
