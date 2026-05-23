---
title: 教程
slug: /tutorials
---

# 教程

## 五分钟打造无磁盘 mini PC 路由器

启动 routerd Live ISO，按照文字向导回答配置问题，再将配置保存到 USB 闪存盘。
如此一来，无需在内置磁盘上安装操作系统，即可将小型 x86 mini PC 打造成可长期使用的路由器。

[开始无磁盘教程](/docs/tutorials/diskless-minipc-walkthrough)

![无磁盘 mini PC 流程](/img/routerd-diskless-minipc.svg)

## 按目标选择

| 目标 | 教程 |
| --- | --- |
| 从 release archive 安装 routerd | [安装](/docs/tutorials/install) |
| 用 YAML 创建第一台路由器 | [入门](/docs/tutorials/getting-started) |
| 配置 WAN 获取与隧道 | [WAN 侧服务](/docs/tutorials/wan-side-services) |
| 配置 LAN 的 DHCP、DNS、RA、NTP | [LAN 侧服务](/docs/tutorials/lan-side-services) |
| 加入保守的防火墙基本配置 | [基本防火墙](/docs/tutorials/basic-firewall) |
| 从 NixOS 开始 | [从 NixOS 开始](/docs/tutorials/nixos-getting-started) |
| 从 FreeBSD 开始 | [从 FreeBSD 开始](/docs/tutorials/freebsd-getting-started) |

routerd 的特色在于，使用同一套资源模型，既能描述虚拟 SDN/VNET 之间的路由器，
也能描述无磁盘物理 mini PC 路由器。
从适合自己的入门教程开始，即便日后网络规模扩大，也能持续沿用相同的资源定义。
