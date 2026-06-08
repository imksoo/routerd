---
title: CloudEdge SAM - OCI Ubuntu 映像的防火牆啟動引導
---

# CloudEdge SAM：OCI Ubuntu 映像的防火牆啟動引導

![OCI Ubuntu 來賓防火牆預設阻擋 WireGuard 和 SAM 轉發，以及所需的啟動引導許可和 routerctl doctor 檢查](/img/diagrams/how-to-cloudedge-sam-oci-firewall-bootstrap.png)

> 實驗性功能（CloudEdge SAM）。這是**主機啟動引導/提供者映像行為**問題，不是 routerd 資料平面的問題。適用於作為 SAM 路由器使用的 OCI Canonical Ubuntu 映像。

## 症狀

在 OCI 中，Canonical Ubuntu 24.04 映像在啟動時啟用了 `iptables-nft` 過濾規則，**reject SSH/ICMP 以外的輸入流量，並 reject 所有 FORWARD 流量**。在此預設設定下，SAM 路由器：

- 即使 OCI 安全清單允許 `UDP/51820` 且 VNIC 設定了 `skipSourceDestCheck=true`，也**無法**接收 WireGuard 交握 — 主機防火牆在輸入 WireGuard 封包到達 `wg-hybrid` 監聽器之前就將其丟棄。
- **無法**轉發捕獲/overlay 流量 — 預設的 `FORWARD` reject 阻擋了 VNIC 介面和 `wg-hybrid` 之間的 SAM 交付路徑。

這與雲端安全清單 / VNIC 的 source-dest-check 無關。它們在 fabric 層運作。**來賓 OS 防火牆**是獨立的層，需要個別許可 SAM 路徑。

## 所需許可（來賓 OS）

在每台 OCI SAM 路由器上，確保主機防火牆許可以下內容：

- 到 `wg-hybrid` WireGuard 監聽器的**輸入 `UDP/51820`**。
- OCI VNIC 介面（如 `ens3`）和 `wg-hybrid` 之間的雙向 **`FORWARD`**。

這些應作為路由器組態中主機啟動引導的一部分以宣告方式記述，而不是依賴臨時的 `iptables` 規則（重建時會遺失）（與其他「路由器前提條件」一樣，要能在乾淨主機上證明）。

## 診斷方法

`routerctl doctor hybrid` 會偵測來賓防火牆中阻擋 WireGuard / SAM 路徑的 reject-all `FORWARD`/`INPUT` 模式，因此許可遺漏會以報告形式顯示，而不是靜默的「無交握」。部署後在 OCI 路由器上執行：

```
routerctl doctor hybrid
```

如果 WireGuard 端點未顯示交握但對端正在傳送 keepalive，請先檢查來賓防火牆（本 How-to），然後檢查 OCI 安全清單，再檢查 VNIC 的 source-dest-check。

## 相關內容

- [Selective Address Mobility](../reference/selective-address-mobility)
- OCI Ubuntu 映像的預設 `iptables-nft` 設定與 AWS/Azure 映像不同。AWS/Azure 的 SAM 冒煙測試未出現此問題，是因為那些映像預設不會對 `FORWARD` 做 reject-all。
