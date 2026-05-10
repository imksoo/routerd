---
title: 教程
slug: /tutorials
---

# 教程

## 五分钟做出无磁盘 mini PC 路由器

启动 routerd live ISO，回答文本配置向导，并把配置保存到 USB。
这样可以在不安装内置磁盘 OS 的情况下，把小型 x86 mini PC 变成可持久使用的路由器。

[开始无磁盘教程](/docs/tutorials/diskless-minipc-walkthrough)

![无磁盘 mini PC 流程](/img/routerd-diskless-minipc.svg)

## 按目标选择

| 目标 | 教程 |
| --- | --- |
| 从 release archive 安装 routerd | [Install](/docs/tutorials/install) |
| 用 YAML 建立第一台路由器 | [Getting started](/docs/tutorials/getting-started) |
| 配置 WAN 获取和 tunnel | [WAN-side services](/docs/tutorials/wan-side-services) |
| 配置 LAN DHCP、DNS、RA、NTP | [LAN-side services](/docs/tutorials/lan-side-services) |
| 添加保守的 firewall baseline | [Basic firewall](/docs/tutorials/basic-firewall) |
| 从 NixOS 开始 | [NixOS getting started](/docs/tutorials/nixos-getting-started) |
| 从 FreeBSD 开始 | [FreeBSD getting started](/docs/tutorials/freebsd-getting-started) |

routerd 的少见之处在于，同一个 resource model 可以描述虚拟 SDN/VNET
之间的路由器，也可以描述无磁盘实体 mini PC 路由器。
