---
title: Basic NAT and firewall policy
sidebar_position: 6
---

# Basic NAT and firewall policy

routerd applies IPv4 NAPT (NAT44) and a stateful firewall to a Linux router. This tutorial shows the minimum resources needed to put both in place on a freshly installed host.

## Scenario

The router has:

- An upstream interface (`wan`) carrying IPv4 connectivity (any of: native dual-stack, PPPoE, or DS-Lite).
- A LAN interface (`lan`) serving local clients with private addresses.
- Optionally, a management interface (`mgmt`).

The goal is to:

- Masquerade outbound IPv4 from the LAN.
- Apply a sane default firewall posture (WAN cannot reach LAN, LAN can reach WAN, management can reach the router itself).

## NAT44

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata:
    name: lan-to-wan
  spec:
    outboundInterface: wan
    sourceCIDRs:
      - 192.0.2.0/24
    masquerade: true
```

routerd renders the rule into the `routerd_nat` nftables table. The same shape works for plain DHCP-acquired uplinks, PPPoE pseudo-interfaces, and DS-Lite tunnels — only `outboundInterface` changes.

## Conntrack observation

routerd consumes conntrack so the Web Console and `routerctl connections` can show live flows. Where `/proc/net/nf_conntrack` is missing, routerd falls back to a sysctl-derived summary; it does not stop on this condition, but the per-flow detail will be unavailable.

## Firewall kinds

`FirewallZone`, `FirewallPolicy`, and `FirewallRule` describe a stateful filter. routerd renders them into the `inet routerd_filter` nftables table.

The roles (`untrust`, `trust`, `mgmt`) provide the implicit accept/drop matrix. routerd injects internal openings for managed services (DHCP, DNS, DS-Lite control traffic, etc.) so they keep working when the firewall is on.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata: {name: wan}
  spec:
    role: untrust
    interfaces:
      - Interface/wan

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata: {name: lan}
  spec:
    role: trust
    interfaces:
      - Interface/lan

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallPolicy
  metadata: {name: default}
  spec: {}
```

For exceptions to the implicit matrix, use the [firewall rule guide](../how-to/firewall-rule.md).

## Validation

```sh
routerctl describe NAT44Rule/lan-to-wan
routerctl firewall test from=wan to=lan proto=tcp dport=22
nft list table inet routerd_filter
nft list table ip routerd_nat
```

## See also

- [Define firewall zones](../how-to/firewall-zone.md)
- [Add firewall exceptions](../how-to/firewall-rule.md)
- [Firewall concept](../concepts/firewall.md)
