---
title: Add firewall exceptions
---

# Add firewall exceptions

## Scenario

The role-based defaults from `FirewallZone` cover the common case, but you need an exception:

- Allow SSH from a specific management subnet.
- Open a service port on the router itself (a metrics endpoint, a custom listener).
- Permit a specific LAN host to receive inbound connections from the WAN (port forward / DMZ-style).

## How routerd solves it

`FirewallRule` declares an exception that overrides the implicit role matrix. Rules are evaluated **before** the implicit matrix, and routerd-derived internal openings (DHCP, DNS, DHCPv6-PD, DS-Lite control traffic, etc.) are evaluated before user rules. That ordering keeps managed services alive even when you add restrictive rules.

## Example: allow SSH from the management network

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: allow-admin-ssh
  spec:
    fromZone: management
    toZone: self
    protocol: tcp
    port: 22
    action: accept
```

`fromZone` and `toZone` reference `FirewallZone` names. `toZone: self` means traffic terminated by the router itself (as opposed to forwarded traffic).

## Example: open a service port on the router

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: allow-metrics
  spec:
    fromZone: lan
    toZone: self
    protocol: tcp
    port: 9100
    action: accept
```

## Example: allow one management host from the LAN

Use `destinationCIDRs` when the exception should apply to a specific host
inside the destination zone. This keeps the rest of the management segment
closed by the role matrix.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: allow-lan-to-router04-webconsole
  spec:
    fromZone: lan
    toZone: management
    destinationCIDRs:
      - 192.168.123.126/32
    protocol: tcp
    port: 8080
    action: accept
```

## Validating before apply

Use the local simulator to check what the rule would do before you apply it:

```sh
routerctl firewall test from=wan to=self proto=tcp dport=22
routerctl describe firewall
```

The first command reports `accept` or `drop` for the specific 5-tuple. The second prints the full effective ruleset including role-matrix defaults and managed openings.

## See also

- [Define firewall zones](./firewall-zone.md)
- [Firewall concept](../concepts/firewall.md)
