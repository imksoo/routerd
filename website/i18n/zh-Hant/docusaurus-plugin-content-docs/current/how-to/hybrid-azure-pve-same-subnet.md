---
title: Azure 與 PVE 的 same-subnet SAM 冒煙測試
---

# Azure 與 PVE 的 same-subnet SAM 冒煙測試

![Azure provider-secondary-IP 捕獲、on-prem proxy-ARP 捕獲、SAM /32 交付路由、轉發檢查、routerctl doctor 驗證的流程](/img/diagrams/how-to-hybrid-azure-pve-same-subnet.png)

本指南總結了經過驗證的運作模式：Azure 的 routerd 節點與本機 Proxmox VE 的 routerd 節點透過 Selective Address Mobility (SAM) 交換選定的 `/32` 位址。資源語義請參見[選擇性位址行動性參考](../reference/selective-address-mobility)。

## Azure 側

- Azure NIC 次要 IP 保留在 Azure 側。這個 provider-side 物件捕獲發往 on-prem `/32` 的封包。
- 不要讓 Ubuntu 來賓 OS 持有已捕獲的 `/32`。cloud-init 或 netplan 可能會自動為次要 NIC IP 指派位址。請抑制或刪除該設定。當 Claim 設定 `configureOSAddress: false` 時，routerd 在 reconcile 時會從本機介面 de-assign 該特定位址，並維持位址不存在的狀態。
- 在 Azure NIC 和 Linux 上都啟用 IP forwarding（`net.ipv4.ip_forward=1`）。

## 本機 PVE 側

- 在能看到 local same-subnet 主機的 LAN/bridge 介面上使用 `proxy-arp` 捕獲。
- 啟用 Linux forwarding。SAM 中 routerd 透過常規 sysctl 路徑啟用 `ip_forward` 和 `proxy_arp`。
- 在 capture 介面和 WireGuard tunnel 之間，透過防火牆政策允許已捕獲 `/32` 的轉發。SAM 不新增防火牆規則或 NAT 規則。
- 對於雲端來賓映像，在判斷 provider fabric 丟棄封包之前，也要檢查主機防火牆的預設值。路由器需要接受 WireGuard 的 UDP listen port，並允許 capture 介面和 `wg-hybrid` 之間的轉發。`routerctl doctor hybrid` 會警告終端 iptables drop/reject 模式和 SAM MSS clamp 規則缺失。

## 隧道與路由

- WireGuard 從 on-prem 向 Azure public IP 撥號。
- 在 on-prem peer 上設定 `persistentKeepalive`，以維持 NAT 和 cloud edge 狀態。
- 首次冒煙測試不使用 UDR。如果後續新增 UDR fallback，請注意 Azure 可能將已捕獲 `/32` 回送到交付源路由器形成 same-subnet 迴圈。
- SAM 交付將每個 claim lower 為指向 tunnel 介面的 `/32` 路由。不變更預設路由。

## 驗證

執行：

```sh
routerctl doctor hybrid
```

對於 `provider-secondary-ip` + `configureOSAddress: false`，確認已捕獲的 `/32` 不存在於本機 `ip addr` 中、交付路由指向 tunnel、`ip_forward=1`。對於 `proxy-arp`，確認 `proxy_arp=1`、proxy neighbor、指向 tunnel 的交付路由、`ip_forward=1`。

在低 MTU overlay 中，`doctor hybrid` 會報告 SAM MSS clamp，確認 `nft list table inet routerd_mss` 中包含選定 `/32` 路徑的 capture-to-tunnel 和 tunnel-to-capture 雙向規則。
