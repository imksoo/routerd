---
title: 基本 NAT 与防火墙
sidebar_position: 6
---

# 基本 NAT 与防火墙：先理解边界

![WAN、LAN、NAT44Rule、FirewallZone 和 FirewallPolicy 的 routerd 入门关系图](/img/diagrams/tutorial-basic-firewall.png)

NAT 和防火墙不是同一个东西：

- **NAT44** 让私有 LAN 地址借用 WAN 的 IPv4 地址访问外部网络。
- **防火墙** 决定哪些流量可以经过或到达路由器。

本页说明资源形状，不把它当作可直接暴露到互联网的生产安全方案。当前重点是 Linux 的 nftables 路径；Ubuntu Server 是主要入门平台。

## 一个现代 NAT44Rule

假设已经有名为 `wan` 和 `lan` 的 `Interface` 资源，LAN 网段是 `192.168.10.0/24`。现代字段写法如下：

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata:
    name: lan-to-wan
  spec:
    type: masquerade
    egressInterface: wan
    sourceRanges:
      - 192.168.10.0/24
    excludeDestinationCIDRs:
      - 10.0.0.0/8
      - 172.16.0.0/12
      - 192.168.0.0/16
```

`type: masquerade` 表示使用出接口当前的 IPv4 地址；`egressInterface` 是逻辑接口资源名，不是随手猜的网卡名。`excludeDestinationCIDRs` 让发往常见私有网段的流量不被伪装，避免把本应走内部路由的流量误当作互联网流量。

## 防火墙资源表达什么

下面是 WAN/LAN 区域和一个策略资源的最小形状：

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: wan
  spec:
    role: untrust
    interfaces:
      - wan

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: lan
  spec:
    role: trust
    interfaces:
      - lan

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallPolicy
  metadata:
    name: home
  spec:
    logDeny: true
```

`FirewallZone` 使用角色 `untrust`、`trust`、`mgmt` 描述网络位置；`FirewallPolicy` 放置全局行为，例如拒绝日志。NAT44 和过滤使用不同的 nftables 表，因此“已经有 NAT”不等于“已经审查并保护了所有入口流量”。

:::caution 防火墙仍处于基础实现阶段
routerd 是预发布软件。这里的区域和策略资源不是通用防火墙语言，也不是安全认证。不要仅凭这段 YAML 就宣称 WAN 一定无法访问 LAN，或将机器暴露到公网。真实部署前需要针对接口、管理入口、VLAN、端口转发和回程流量做独立审查与测试。
:::

## 安全检查配置

先让 `routerd` 直接验证完整文件，并使用临时路径 dry-run：

```bash
routerd validate --config router.yaml
workdir=$(mktemp -d)
routerd apply --once --dry-run --skip-service-manager --config router.yaml --status-file "$workdir/status.json" --state-file "$workdir/state.db" --ledger-file "$workdir/ledger.db" --netplan-file "$workdir/50-routerd.yaml" --dnsmasq-file "$workdir/dnsmasq.conf" --dnsmasq-service-file "$workdir/routerd-dnsmasq.service" --nftables-file "$workdir/routerd-nat.nft"
```

只有在 `routerd serve` 已经通过控制台或独立管理路径启动后，才用 `routerctl` 查询运行结果：

```bash
sudo routerctl get status
sudo routerctl describe NAT44Rule/lan-to-wan
sudo nft list table ip routerd_nat
```

## 继续阅读

- [基本 IPv4 NAT 网关](../config-examples/basic-ipv4-nat.md)
- [防火墙概念](../concepts/firewall.md)
- [支持的平台](../platforms.md)
