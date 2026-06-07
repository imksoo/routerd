---
title: 防火牆速率限制與 ICMP 規則
---

# 防火牆速率限制與 ICMP 規則

![WAN traffic class、FirewallRule rate 與 connection limit，以及產生的 stateful nftables filtering](/img/diagrams/config-example-firewall-rate-limit.png)

此範例示範小型路由器的有狀態 `FirewallRule` 撰寫方式。

- 以單一多連接埠規則允許 HTTP 和 HTTPS
- 僅允許來自 WAN 的 ICMP echo request
- 對超過封包速率或每來源連線數上限的 SSH 嘗試執行 reject

完整的 YAML 位於 `examples/firewall-rate-limit.yaml`。

## 套用步驟

```bash
routerctl validate --config examples/firewall-rate-limit.yaml
routerctl plan --config examples/firewall-rate-limit.yaml
routerctl apply --config examples/firewall-rate-limit.yaml --dry-run
```

## 規則摘錄

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
