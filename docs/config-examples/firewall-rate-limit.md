---
title: Firewall rate limits and ICMP rules
---

# Firewall rate limits and ICMP rules

This example shows stateful `FirewallRule` expressions for a small router:

- allow HTTP and HTTPS on the router with one multi-port rule
- allow only ICMP echo requests from the WAN
- reject SSH attempts that exceed a packet rate or per-source connection limit

The complete YAML is in `examples/firewall-rate-limit.yaml`.

## Apply sequence

```bash
routerd validate --config examples/firewall-rate-limit.yaml
routerd plan --config examples/firewall-rate-limit.yaml
routerd apply --config examples/firewall-rate-limit.yaml --once --dry-run
```

## Rule excerpt

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: ssh-bruteforce-over-limit
  spec:
    fromZone: wan
    toZone: self
    protocol: tcp
    destinationPorts:
      - "22"
    action: reject
    rateLimit:
      rate: 8
      burst: 16
      unit: packet
      per: minute
      log: true
    connLimit:
      maxPerSource: 4
      log: true
```
