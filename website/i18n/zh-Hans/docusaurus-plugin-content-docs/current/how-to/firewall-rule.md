---
title: 新增防火墙例外规则
---

# 新增防火墙例外规则

![FirewallRule exception 处理 management SSH、router service port、scoped destination CIDR，以及 implicit role matrix 前的评估顺序](/img/diagrams/how-to-firewall-rule.png)

## 适用场景

`FirewallZone` 的角色式默认已能满足大多数需求，但有时仍需要例外处理。

- 希望允许来自特定管理子网的 SSH 连接。
- 需要开放路由器本机上的服务端口（如 metrics 端点、自定义 listener）。
- 需要让 WAN 的 inbound 连接通往某台特定的 LAN 主机（类似 port forward 或 DMZ 的用途）。

## routerd 的解决方式

使用 `FirewallRule` 声明例外，覆盖隐含的角色矩阵。
规则的评估优先于角色矩阵，而 routerd 自动派生的内部通行孔（DHCP、DNS、DHCPv6-PD、DS-Lite 控制等）又优先于用户规则。
这个顺序确保即使新增限制规则，受管服务仍能正常运作。

## 示例：允许来自管理网络的 SSH

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

`fromZone` / `toZone` 参照 `FirewallZone` 的名称。
`toZone: self` 表示路由器本身终止的通信（非 forward）。

## 示例：开放路由器本机的服务端口

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: allow-metrics
  spec:
    fromZone: lan
    toZone: self
    protocol: tcp
    destinationPorts:
      - "9100"
    action: accept
```

## 示例：仅允许 LAN 访问管理区域中的特定主机

若只需对目的区域内的特定主机开例外，可指定 `destinationCIDRs`，
无需开放整个管理区域。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: allow-lan-to-admin-console
  spec:
    fromZone: lan
    toZone: management
    destinationCIDRs:
      - 192.0.2.126/32
    protocol: tcp
    destinationPorts:
      - "8080"
    action: accept
```

## 示例：多个 Web 端口与 ICMP echo

若需在单一规则中处理多个 TCP / UDP 端口，请使用 `destinationPorts`。
ICMP 规则可依 type 名称进行过滤。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: wan-web
  spec:
    fromZone: wan
    toZone: self
    protocol: tcp
    destinationPorts:
      - "80"
      - "443"
    action: accept

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallRule
  metadata:
    name: wan-icmp-echo
  spec:
    fromZone: wan
    toZone: self
    protocol: icmp
    icmpType: echo-request
    action: accept
```

## 示例：拒绝超过速率／连接限制的 SSH

`rateLimit` 会匹配超过设定阈值的流量；`connLimit` 则在相同来源已持有超过允许数量的并行跟踪状态时匹配。

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

## 应用前的确认

请先在本机模拟器确认行为，再正式应用。

```sh
routerctl firewall test from=wan to=self proto=tcp dport=22
routerctl describe firewall
```

第一个命令针对指定的 5-tuple 返回 `accept` / `drop`。
第二个命令显示包含角色矩阵默认值与受管通行孔在内的完整有效规则。

## 相关项目

- [定义防火墙区域](./firewall-zone.md)
- [防火墙概念说明](../concepts/firewall.md)
