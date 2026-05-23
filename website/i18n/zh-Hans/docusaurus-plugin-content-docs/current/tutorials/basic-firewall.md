---
title: 基本 NAT 与防火墙策略
sidebar_position: 6
---

# 基本 NAT 与防火墙策略

routerd 在 Linux 路由器上应用 IPv4 NAPT（NAT44）与 stateful 防火墙。
本教程演示在初始配置的主机上同时启用两者的最小步骤。

## 默认配置

假设路由器的配置如下：

- 承载 IPv4 的上游接口（`wan`）。原生双栈、PPPoE 或 DS-Lite 均可。
- 向 LAN 内客户端分配 private 地址的 LAN 接口（`lan`）。
- 可选的管理接口（`mgmt`）。

本教程的目标有两个：

- 对 LAN 发往外部的 IPv4 流量进行 masquerade。
- 应用健全的防火墙默认状态（WAN 无法到达 LAN、LAN 可以到达 WAN、管理端可以到达路由器本身）。

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

routerd 会在 `routerd_nat` nftables 表中生成规则。
无论是 DHCP 获取的线路、PPPoE 虚拟接口还是 DS-Lite 隧道，写法相同，只需更改 `outboundInterface`。

## conntrack 观测

routerd 读取 conntrack，并在 Web 管理界面与 `routerctl connections` 中显示实时流量。
若环境中没有 `/proc/net/nf_conntrack`，则回退为以 sysctl 为基础的摘要。不会视为失败，仅显示可观测的范围。

## Firewall Kind

`FirewallZone`、`FirewallPolicy`、`FirewallRule` 用于表达 stateful 过滤条件。
routerd 会将这些资源生成至 `inet routerd_filter` nftables 表。

角色（`untrust`、`trust`、`mgmt`）提供隐含的 accept / drop 矩阵。
DHCP、DNS、DS-Lite 控制等受管理服务所需的通道，routerd 会自动开放。

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

需要添加例外时，请参阅[防火墙规则指南](../how-to/firewall-rule.md)。

## 确认

```sh
routerctl describe NAT44Rule/lan-to-wan
routerctl firewall test from=wan to=lan proto=tcp dport=22
nft list table inet routerd_filter
nft list table ip routerd_nat
```

## 相关项目

- [定义防火墙区域](../how-to/firewall-zone.md)
- [添加防火墙例外](../how-to/firewall-rule.md)
- [防火墙概念](../concepts/firewall.md)
