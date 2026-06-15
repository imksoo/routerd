---
title: What is CloudEdge SAM
---

# What is CloudEdge SAM

CloudEdge SAM (Selective Address Mobility) is the routerd capability that treats
**a chosen set of `/32` IPv4 addresses as portable "street addresses" that move
between on-prem and multiple public clouds (AWS / Azure / OCI)**.

This is a concept you will not find on an ordinary router or cloud load
balancer, so we start with *what is new* and *what problem it solves*. Without
that, the configuration fields make little sense.

## The problem

When you want to make a service redundant across clouds, the traditional options
both hurt:

1. **L2 extension (stretching a LAN with VXLAN/EVPN, etc.)** — public clouds do
   not expose an operator-controlled broadcast domain. Each cloud fabric has its
   own routing and address-ownership model, and you cannot simply stretch an
   Ethernet segment into it.
2. **DNS failover or a global load balancer** — the IP address the client sees
   changes. TCP connections break, DNS TTL caches linger, and any client that
   holds an IP address directly cannot follow.

CloudEdge SAM takes a **third path**: instead of moving "a whole LAN" it moves
"only the `/32` addresses you want to move", over a routerd-to-routerd overlay.
The source and destination IP addresses the client sees are preserved, so even
when the node that holds an address (the *holder*) moves from AWS to Azure,
**the same IP address keeps living**.

You can read this as "a virtual IP that moves across clouds". Where a VRRP
virtual IP can only move within one L2 segment, a CloudEdge SAM `/32` moves
**across cloud boundaries**.

## The mental model

The key to understanding CloudEdge SAM is to **separate two planes**.

| Plane | Responsibility | Source of truth |
| --- | --- | --- |
| **Reachability (overlay)** | which node owns an address, and where to carry its packets | the **BGP best path** ([ADR 0012](../adr/0012-bgp-address-mobility.md)) |
| **Cloud ingress** | whether the cloud fabric admits external packets to the right VM | **provider secondary IP / route tables** (reconciled in the background) |

The point is the division of labor: **the BGP RIB owns the truth of
reachability, and cloud API operations only follow it after the fact**. Older
routerd did this with a bespoke control plane of lease tables and epochs; today
it leans on plain BGP unicast `/32` (see
[ADR 0012](../adr/0012-bgp-address-mobility.md)).

```
            pick only the "addresses to move" out of an on-prem /24
                              │
            ┌─────────────────┼─────────────────┐
            ▼                 ▼                 ▼
        ┌────────┐        ┌────────┐        ┌────────┐
        │ AWS    │        │ Azure  │        │ OCI    │   ← routerd nodes
        │ routerd│◄──────►│ routerd│◄──────►│ routerd│   (WireGuard/IPIP overlay + BGP)
        └────────┘        └────────┘        └────────┘
            ▲ holder          standby           standby
            │
        the node winning the BGP best path for a /32 is its current holder
```

## The new concepts

A few routerd-specific terms appear. Here are the ones to learn first (the full
internals are in [CloudEdge SAM internals](../reference/cloudedge-sam-internals.md)).

- **MobilityPool** — the single operator-authored resource that declares *which*
  `/32`s move, *across which* nodes, and *how*. Like a BGP peer list, each node
  only needs the *identity* of the others (nodeRef / site / role / placement); it
  does not need their NIC IDs or subnet IDs.
- **capture** — assigning a target `/32` to a cloud VM's NIC as a secondary IP so
  that VM can receive packets for that address. This builds the "cloud ingress".
- **holder** — the node that currently captures a `/32` and advertises it as the
  BGP best path. There is exactly one per placement group.
- **placement group and priority** — the active/standby declaration: "make the
  higher-priority node in this group active for these `/32`s". A **lower priority
  number means higher priority**.
- **holder-beacon** — the BGP community (`64512:121`) that *only the active
  holder* attaches to its owner `/32`. Other nodes decide "only the node
  advertising a best path carrying this community is the real holder". It is the
  **authoritative marker** that prevents a standby's weak advertisement or a
  just-booted advertisement from being mistaken for holdership.

## The switching behavior (this is the value)

The hard problem CloudEdge SAM solves is a pair of conflicting demands: **keep
switching to a minimum, but reliably take over when something truly dies**.
routerd changes behavior based on the priority relationship.

- **Two nodes at equal priority (e.g. a=10, b=10)** → **no-preempt**. Once one
  becomes holder, the other does not take it back even after it returns, so a
  pointless switch never shakes the dataplane.
- **Unequal priority (e.g. a=10 high, b=20 low)** → **auto-restore**. After the
  high-priority node dies and the low-priority node takes over, ownership returns
  to the high-priority node automatically when it comes back — but the `/32`s
  move one at a time, with **zero dataplane dip**.
- **When the active dies (regardless of priority)** → **reliable failover**. If
  the holder's VM stops or its OS fails, a standby seizes the secondary IPs,
  takes over the BGP advertisement, and restores the dataplane automatically.

To reconcile all three, routerd combines the following mechanisms (details in
[internals](../reference/cloudedge-sam-internals.md)).

1. **startup fence** — a just-returned node defers asserting active until its
   observations converge, preventing it from reclaiming holdership using stale
   self-state.
2. **holder retention** — while a node physically holds captures it stays active;
   it does not give up holdership for a deterministic tie-break or a transient
   observation.
3. **holder-beacon** — authoritatively decides "who is the real holder" on the
   BGP best path, also resolving the cold-start mutual-defer deadlock.

## What CloudEdge SAM is not (to avoid confusion)

- **It is not L2 extension.** It does not stretch an Ethernet broadcast domain
  into the cloud; it carries only the chosen `/32`s over the overlay.
- **It is not NAT or a load balancer.** Source and destination IPs are preserved.
  Firewall and NAT are separate routerd layers, not fields on mobility resources.
- **It does not magically solve cloud-native ingress.** Overlay reachability
  recovers from BGP convergence alone, but external ingress entering through a
  VPC/VNet/VCN must wait for the provider secondary IP / route table to catch up.

## What to read next

- [Selective Address Mobility (config model)](../reference/selective-address-mobility.md)
  — how to author `MobilityPool`, self/remote members, capture policy.
- [CloudEdge SAM internals](../reference/cloudedge-sam-internals.md)
  — the BGP community taxonomy, placement, no-preempt, holder-beacon, failover.
- [ADR 0012: BGP /32 Address Mobility](../adr/0012-bgp-address-mobility.md)
  — the decision to make BGP the source of truth.
- [CloudEdge mobility demo](../how-to/cloudedge-mobility-demo.md)
  — a hands-on lab running four sites (on-prem/AWS/Azure/OCI).
