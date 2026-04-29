---
title: NTT FLET'S IPv6 setup
slug: /how-to/flets-ipv6-setup
---

# NTT FLET'S IPv6 setup

This page covers connecting routerd to NTT's FLET'S IPv6 service in
Japan, where the Home Gateway (HGW) is in the path and delegates IPv6
prefixes by DHCPv6-PD.

## Topology this page assumes

- An NTT HGW (e.g. PR-400NE) on the upstream side, with IPv6 enabled.
- Hikari Denwa (NTT IP telephony) subscription. The HGW only runs a
  DHCPv6-PD server when this is provisioned.
- routerd's host on the LAN side of the HGW. Its WAN interface is in the
  HGW's LAN.

## Minimum config

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: Interface
  metadata:
    name: wan
  spec:
    ifname: ens18
    adminUp: true
    managed: true

- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6DHCPAddress
  metadata:
    name: wan-ra
  spec:
    interface: wan

- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6PrefixDelegation
  metadata:
    name: wan-pd
  spec:
    interface: wan
    profile: ntt-hgw-lan-pd
```

The `ntt-hgw-lan-pd` profile pulls in the right defaults:

- DUID type: `link-layer` (MAC-derived). NTT's documented terminal
  models use DUID-LL or DUID-LLT.
- Prefix length: `/60` (the HGW divides the upstream prefix into 16
  sub-prefixes and hands out `/60`s).
- Rapid Commit: disabled. NTT's option tables mark it as unused for
  this profile.
- IA_NA: not requested. NTT's profile is PD-only.

## Lab notes

If you are running routerd on a virtual machine on Linux (e.g. Proxmox)
and the HGW does not seem to reply to your Solicit, check the following
before suspecting the HGW:

- The Linux bridge between the host NIC and the VM has
  `multicast_snooping=0`. With the default `=1`, RA and DHCPv6 multicast
  is silently dropped on some kernels.
- Any L2 switch in the path has IGMP/MLD snooping disabled, or has an
  MLD querier so that snooping tables stay populated.
- `tcpdump` filter is `udp port 546 or udp port 547`. NTT HGWs reply
  from an ephemeral source port, not 547.

These facts are documented in the [design notes](../design-notes) under
"Lab-specific issues".

## Verifying

```bash
routerctl describe ipv6pd/wan-pd
```

Look for `currentPrefix` and a recent `lastObservedAt`. The HGW UI page
"DHCPv6 Server payout status" should also list a row for your router's
MAC.

## Common pitfalls

- Setting an explicit `iaid` and then changing it later. The HGW keeps a
  binding keyed on the client identity; if the client identity changes
  between leases, the old binding ages out instead of being reused. Pick
  an IAID once and keep it.
- Sending DHCPv6 Release on every routerd restart. NTT HGWs handle
  Release immediately, which can disturb the lease table during routine
  apply cycles. routerd's NTT profile does not restart `dhcp6c` for
  unrelated config changes.
- Assuming the prefix you got last time will be the same. NTT HGWs may
  reassign a different `/60` after a fresh acquisition. The
  `lastPrefix` field in routerd state is for diagnostics; do not hard
  code the value into LAN-side YAML.

## See also

- [IPv6PrefixDelegation reference](../reference/api-v1alpha1#ipv6prefixdelegation)
- [Design notes](../design-notes) for the underlying observations.
