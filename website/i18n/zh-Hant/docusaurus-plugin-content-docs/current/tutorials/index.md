---
title: 教學
slug: /tutorials
---

# 教學

## 五分鐘做出無磁碟 mini PC 路由器

啟動 routerd live ISO，回答文字設定精靈，並把設定保存到 USB。
這樣可以在不安裝內建磁碟 OS 的情況下，把小型 x86 mini PC 變成可持久使用的路由器。

[開始無磁碟教學](/docs/tutorials/diskless-minipc-walkthrough)

![無磁碟 mini PC 流程](/img/routerd-diskless-minipc.svg)

## 依目標選擇

| 目標 | 教學 |
| --- | --- |
| 從 release archive 安裝 routerd | [Install](/docs/tutorials/install) |
| 用 YAML 建立第一台路由器 | [Getting started](/docs/tutorials/getting-started) |
| 設定 WAN 取得與 tunnel | [WAN-side services](/docs/tutorials/wan-side-services) |
| 設定 LAN DHCP、DNS、RA、NTP | [LAN-side services](/docs/tutorials/lan-side-services) |
| 加入保守的 firewall baseline | [Basic firewall](/docs/tutorials/basic-firewall) |
| 從 NixOS 開始 | [NixOS getting started](/docs/tutorials/nixos-getting-started) |
| 從 FreeBSD 開始 | [FreeBSD getting started](/docs/tutorials/freebsd-getting-started) |

routerd 的稀少之處在於，同一個 resource model 可以描述虛擬 SDN/VNET
之間的路由器，也可以描述無磁碟實體 mini PC 路由器。
