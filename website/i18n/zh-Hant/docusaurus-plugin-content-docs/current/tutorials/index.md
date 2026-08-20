---
title: 逐步教學
slug: /tutorials
---

# 逐步教學

![routerd 教學路線：網路基礎、安裝、安全檢查、第一台實驗路由器，以及 WAN、LAN 與 NAT](/img/diagrams/tutorial-index.png)

這些教學依照「先理解、再檢查、最後接上真實網路」排序。第一輪請使用隔離的 Ubuntu Server VM，並保留虛擬機主控台、序列主控台或獨立的管理網卡。

| 目標 | 建議閱讀 |
| --- | --- |
| 先弄清楚 WAN、LAN、DHCP、DNS、NAT | [網路基礎](./network-basics.md) |
| 從發布封存檔安裝程式 | [安裝](./install.md) |
| 撰寫第一份 YAML，但不變更網路 | [安全起步](./getting-started.md) |
| 設定 WAN DHCP 與 LAN 閘道位址 | [第一台實驗路由器](./first-router.md) |
| 加入 WAN 取得、PPPoE、DS-Lite 或 DHCPv6-PD | [WAN 側服務](./wan-side-services.md) |
| 加入 LAN DHCP、RA、DNS 或 NTP | [LAN 側服務](./lan-side-services.md) |
| 了解 NAT 和防火牆資源的邊界 | [基本 NAT 與防火牆](./basic-firewall.md) |
| 查看完整設定如何組合 | [設定範例集](../config-examples/index.md) |

## 請記住兩段流程

第一次檢查本機檔案時：

```text
routerd validate  →  routerd apply --once --dry-run
```

前者檢查 YAML；後者在不提交網路變更的情況下跑一次控制器與產生流程。真正的 `routerd serve` 會管理主機，所以一定要在確認管理路徑安全後才啟動。

服務運行後，才使用另一段流程：

```text
routerd serve  →  routerctl get / describe / validate / plan / apply
```

`routerctl` 是執行中 daemon 的客戶端，不能拿來取代離線檔案檢查。

:::caution 有線／無線隔離不會自動出現
後續的 NAT、防火牆與訪客裝置範例用來理解資源模型，但防火牆仍在預發布的基礎實作階段。若訪客裝置和受信任裝置共用同一二層網路，真正的隔離通常需要 VLAN、獨立 SSID 或獨立交換器埠。
:::

## 下一步

從[網路基礎](./network-basics.md)或[安全起步](./getting-started.md)開始。
