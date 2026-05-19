---
title: Firewall rate limit と ICMP rule
---

# Firewall rate limit と ICMP rule

この例は、小規模 router 向けの stateful `FirewallRule` expression を示します。

- HTTP と HTTPS を 1 つの multi-port rule で許可する
- WAN からの ICMP echo request だけを許可する
- packet rate または送信元ごとの connection limit を超えた SSH 試行を reject する

完全な YAML は `examples/firewall-rate-limit.yaml` にあります。

## 適用手順

```bash
routerd validate --config examples/firewall-rate-limit.yaml
routerd plan --config examples/firewall-rate-limit.yaml
routerd apply --config examples/firewall-rate-limit.yaml --once --dry-run
```

## Rule 抜粋

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
