---
title: 防火墙速率限制与 ICMP 规则
---

# 防火墙速率限制与 ICMP 规则

![WAN traffic class、FirewallRule rate 与 connection limit，以及生成的 stateful nftables filtering](/img/diagrams/config-example-firewall-rate-limit.png)

此示例演示小型路由器的有状态 `FirewallRule` 编写方式。

- 以单一多端口规则允许 HTTP 和 HTTPS
- 仅允许来自 WAN 的 ICMP echo request
- 对超过数据包速率或每来源连接数上限的 SSH 尝试执行 reject

完整的 YAML 位于 `examples/firewall-rate-limit.yaml`。

## 应用步骤

```bash
routerd validate --config examples/firewall-rate-limit.yaml
routerd plan --config examples/firewall-rate-limit.yaml
routerd apply --config examples/firewall-rate-limit.yaml --once --dry-run
```

## 规则摘录

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
