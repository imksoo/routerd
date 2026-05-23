---
title: Define firewall zones
---

# Define firewall zones

## Scenario

You want a stateful firewall whose default behaviour is "WAN cannot reach LAN, LAN can reach WAN, management can reach everything." That is the matrix every home or SOHO router needs, and writing it as individual `accept` / `drop` rules is repetitive and error-prone.

## How routerd solves it

`FirewallZone` maps interfaces to a **role**. routerd has a built-in role matrix that derives the directional default actions, so you usually do not need any explicit `FirewallRule` for the common case.

| role | Typical use |
| --- | --- |
| `untrust` | WAN-facing interfaces (uplink, DSLite tunnel, PPPoE pseudo-interface) |
| `trust` | Normal LAN segments |
| `mgmt` | Out-of-band management network |

The implicit matrix is:

| from \ to | self | trust | mgmt | untrust |
| --- | --- | --- | --- | --- |
| `mgmt` | accept | accept | n/a | accept |
| `trust` | accept | accept | drop | accept |
| `untrust` | drop | drop | drop | n/a |
| `self` | accept | accept | accept | accept |

Established/related connections are always allowed.

## Example

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: wan
  spec:
    role: untrust
    interfaces:
      - Interface/wan
      - DSLiteTunnel/ds-lite-primary

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: lan
  spec:
    role: trust
    interfaces:
      - Interface/lan

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: management
  spec:
    role: mgmt
    interfaces:
      - Interface/mgmt
```

This is enough for a typical home router. The role matrix supplies the defaults; you only add explicit `FirewallRule` resources to express exceptions.

## See also

- [Add firewall exceptions](./firewall-rule.md)
- [Isolate guest devices by MAC address](./guest-mode.md)
- [Firewall concept](../concepts/firewall.md)
