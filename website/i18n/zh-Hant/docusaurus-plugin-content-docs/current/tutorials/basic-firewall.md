---
title: 基本的 NAT 與防火牆政策
sidebar_position: 6
---

# 基本的 NAT 與防火牆政策

![包含 WAN、LAN、可選 management、NAT44Rule、FirewallZone、FirewallPolicy 與 nftables validation 的基本 routerd NAT44 與 firewall tutorial 流程](/img/diagrams/tutorial-basic-firewall.png)

routerd 在 Linux 路由器上套用 IPv4 NAPT（NAT44）與 stateful 防火牆。
本教學示範在初始設定的主機上同時啟用兩者的最小步驟。

## 預設構成

假設路由器的構成如下：

- 承載 IPv4 的上游介面（`wan`）。原生雙堆疊、PPPoE 或 DS-Lite 皆可。
- 向 LAN 內用戶端分配 private 位址的 LAN 介面（`lan`）。
- 可選的管理介面（`mgmt`）。

本教學的目標有兩個：

- 對 LAN 發往外部的 IPv4 流量進行 masquerade。
- 套用健全的防火牆預設狀態（WAN 無法到達 LAN、LAN 可以到達 WAN、管理端可以到達路由器本身）。

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

routerd 會在 `routerd_nat` nftables 表中產生規則。
無論是 DHCP 取得的線路、PPPoE 虛擬介面還是 DS-Lite 隧道，寫法相同，只需變更 `outboundInterface`。

## conntrack 觀測

routerd 讀取 conntrack，並在 Web 管理介面與 `routerctl get connections` 中顯示即時流量。
若環境中沒有 `/proc/net/nf_conntrack`，則退回為以 sysctl 為基礎的摘要。不會視為失敗，僅顯示可觀測的範圍。

## Firewall Kind

`FirewallZone`、`FirewallPolicy`、`FirewallRule` 用於表達 stateful 過濾條件。
routerd 會將這些資源產生至 `inet routerd_filter` nftables 表。

角色（`untrust`、`trust`、`mgmt`）提供隱含的 accept / drop 矩陣。
DHCP、DNS、DS-Lite 控制等受管理服務所需的通道，routerd 會自動開放。

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

需要新增例外時，請參閱[防火牆規則指南](../how-to/firewall-rule.md)。

## 確認

```sh
routerctl describe NAT44Rule/lan-to-wan
routerctl firewall test from=wan to=lan proto=tcp dport=22
nft list table inet routerd_filter
nft list table ip routerd_nat
```

## 相關項目

- [定義防火牆區域](../how-to/firewall-zone.md)
- [新增防火牆例外](../how-to/firewall-rule.md)
- [防火牆概念](../concepts/firewall.md)
