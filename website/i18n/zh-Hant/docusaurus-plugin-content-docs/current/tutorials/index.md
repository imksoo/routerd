---
title: 教學
slug: /tutorials
---

# 教學

## 五分鐘做出無磁碟 mini PC 路由器

啟動 routerd Live ISO，依照文字精靈回答設定問題，再將設定儲存到 USB 隨身碟。
如此一來，無需在內建磁碟安裝作業系統，即可將小型 x86 mini PC 打造成可長期使用的路由器。

[開始無磁碟教學](/docs/tutorials/diskless-minipc-walkthrough)

![無磁碟 mini PC 流程](/img/routerd-diskless-minipc.svg)

## 依目標選擇

| 目標 | 教學 |
| --- | --- |
| 從 release archive 安裝 routerd | [安裝](/docs/tutorials/install) |
| 用 YAML 建立第一台路由器 | [入門](/docs/tutorials/getting-started) |
| 設定 WAN 取得與通道 | [WAN 側服務](/docs/tutorials/wan-side-services) |
| 設定 LAN 的 DHCP、DNS、RA、NTP | [LAN 側服務](/docs/tutorials/lan-side-services) |
| 加入保守的防火牆基本設定 | [基本防火牆](/docs/tutorials/basic-firewall) |
| 從 NixOS 開始 | [從 NixOS 開始](/docs/tutorials/nixos-getting-started) |
| 從 FreeBSD 開始 | [從 FreeBSD 開始](/docs/tutorials/freebsd-getting-started) |

routerd 的特色在於，使用同一套資源模型，既能描述虛擬 SDN/VNET 之間的路由器，
也能描述無磁碟實體 mini PC 路由器。
從適合自己的入門教學開始，即便日後網路規模擴大，也能持續沿用相同的資源定義。
