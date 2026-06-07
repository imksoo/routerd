---
title: 定义防火墙区域
---

# 定义防火墙区域

![FirewallZone 定义 WAN、LAN、management network 的 interface role 与 stateful role-matrix default 的流程](/img/diagrams/how-to-firewall-zone.png)

## 适用场景

「WAN 无法到达 LAN、LAN 可以到达 WAN、管理路径可以到达所有地方」，这是家庭与 SOHO 路由器的基本策略矩阵。
若以单独的 `accept` / `drop` 规则来编写，不仅重复性高，也容易出错。

## routerd 的解决方式

使用 `FirewallZone` 将接口与**角色（role）**绑定。
routerd 会根据内置的角色矩阵，自动推导各方向的默认动作，因此在典型配置下，甚至不需要编写任何 `FirewallRule`。

| role | 用途 |
| --- | --- |
| `untrust` | WAN 侧（上游线路、DSLite 隧道、PPPoE 虚拟接口） |
| `trust` | 一般 LAN 区段 |
| `mgmt` | 带外管理网络 |

隐含的矩阵如下：

| from \ to | self | trust | mgmt | untrust |
| --- | --- | --- | --- | --- |
| `mgmt` | accept | accept | n/a | accept |
| `trust` | accept | accept | drop | accept |
| `untrust` | drop | drop | drop | n/a |
| `self` | accept | accept | accept | accept |

established/related 的连接一律允许。

## 示例

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

对于典型的家庭路由器，这样就已足够。`FirewallRule` 只在需要表达例外时才新增。

## 相关项目

- [新增防火墙例外规则](./firewall-rule.md)
- [以 MAC 地址隔离访客设备](./guest-mode.md)
- [防火墙概念说明](../concepts/firewall.md)
